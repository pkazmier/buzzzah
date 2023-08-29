package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
	"slices"
)

// gameShow enables receiving and broadcasting Messages between subscribers.
type gameShow struct {
	// logf controls where logs are sent. Defaults to log.Printf.
	logf func(f string, v ...interface{})

	// subscriberMsgBuffer controls the max number of messages that can be
	// queued for sending to a subscriber before it is kicked. It also
	// controls the inbound queue size. Defaults to 32.
	subscriberBuffer int

	// publishLimiter controls the rate limit applied to the subcribers
	// publishing messages. Defaults to one publish every 1ms with a
	// burst of 100.
	publishLimiter *rate.Limiter

	// serveMux routes the various HTTP endpoints to their handlers.
	serveMux http.ServeMux

	// hostTeamName is the name of the team whose members are allowed to
	// send messages designated only for hosts such as resetBuzzerMessage
	// and scoreChangeMessage. Hosts are not allow to participate in the
	// game as a player.
	hostTeamName string

	// gameStateMu is the mutex that protects all of the internal game
	// stote resources: buzzed, score, token2user, subscribers, pending.
	gameStateMu sync.Mutex

	// token2user is a map of tokens to Users (name and team). When a user
	// logins via the login page, the server generates a token for them
	// and sends it via a HTTP cookie. This cookie is used to identify the
	// name and team of the user upon subsequent HTTP calls.
	//
	// Tokens are never removed from the map as this server is currently
	// intended only to be used for a single game and thus is terminated.
	token2user map[string]user

	// pending is a map of user names or team names that a user has
	// submitted via the login form, but have not yet established their
	// websocket connection. This helps prevent two users that are logging
	// in at at the same from having collisions of any sort.
	pending map[string]struct{}

	// subscribers is a map of user names to subscribers.
	//
	// One invariant is that this map only contain active users. If a name
	// exists in this map, there is an active subscriber connected.
	//
	// Another invariant is that a user name must not be used as a team
	// name and vice versa. The UI shares the namespace between users and
	// team names.
	subscribers map[string]*subscriber

	// score is a map of team names and their respective score. It serves
	// as the authoratative source of active teams in the current game. If
	// a subscriber joins the game to a new team, it is added. Similarly,
	// if the last member of a team leaves the game, it is removed.
	//
	// The special team name designated as the hostTeam is not reflected
	// in the scoreboard as hosts cannot participate as players.
	//
	// An invariant held throughout is that a team name must not be used
	// as a user name and vice versa. The UI shares the namespace between
	// users and teams names.
	score map[string]int

	// buzzed is the ordered list of subscriber names that have pushed
	// their buzzer. The first element buzzed in first, while the last
	// buzzed in last.
	//
	// An invariant held throughout is that this list will only contain
	// names of subscribers with an established websocket. I.e. if a
	// subscriber disconnects while they have buzzed in, it is removed.
	buzzed []string
}

// newGameShow constructs a gameShow with defaults. The hostTeamName parameter
// specifies the team name that will identify users serving as hosts. Hosts
// should join the game using this team name.
func newGameShow(hostTeamName string) *gameShow {
	gs := &gameShow{
		logf:             log.Printf,
		subscriberBuffer: 32,
		publishLimiter:   rate.NewLimiter(rate.Every(time.Millisecond*1), 100),
		hostTeamName:     hostTeamName,
		token2user:       make(map[string]user),
		pending:          make(map[string]struct{}),
		subscribers:      make(map[string]*subscriber),
		score:            make(map[string]int),
		buzzed:           []string{}, // not nil for JSON serialization
	}
	gs.serveMux.Handle("/", http.FileServer(http.Dir(".")))
	gs.serveMux.HandleFunc("/login", gs.loginHandler)
	gs.serveMux.HandleFunc("/join", gs.subscribeHandler)
	return gs
}

// subscriber represents a user with an active websocket and the three
// channels used to process errors, incoming, and outgoing messages.
type subscriber struct {
	user
	errc     chan error
	incoming chan any
	outgoing chan any
	conn     *websocket.Conn
}

// closeSlow closes the websocket with an error code stating the client was
// too slow to keep up with messages that were being sent to it.
func (s *subscriber) closeSlow() {
	s.conn.Close(websocket.StatusPolicyViolation, "connection too slow to keep up with messages")
}

// isHost returns true if the subscriber is a host, otherwise false.
func (gs *gameShow) isHost(s *subscriber) bool {
	return s.Team == gs.hostTeamName
}

// ServeHTTP dispatches requests to the appropriated handlers.
func (gs *gameShow) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	gs.serveMux.ServeHTTP(w, r)
}

// loginHandler processes incoming login requests. Upon successful login, the
// handler generates a token, which is sent to the client via a "token"
// cookie. in addition, a "host" cookie is also included that will be "true"
// if the subscriber joined the hostTeamName. Finally, an HTTP redirect is
// sent to the client if successful, otherwise an HTTP error is sent.
//
// Login requests must include the HTTP query parameters "name" and "team".
// Names and teams share the same namespace so they must be unique with each
// other as well as all other users and teams already logged into the server.
func (gs *gameShow) loginHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	name := query.Get("name")
	team := query.Get("team")

	if name == "" || team == "" {
		http.Error(w, "both name and team must be provided", http.StatusConflict)
		return
	}

	if name == team {
		http.Error(w, "name and team must be different, pick another name", http.StatusConflict)
		return
	}

	gs.gameStateMu.Lock()
	defer gs.gameStateMu.Unlock()

	// Check to see if someone else has already logged in with the same
	// name, but has not yet become an active subscriber. Recall, both
	// name and team names share a namespace and must be unique.
	for pendingNameOrTeam := range gs.pending {
		if pendingNameOrTeam == name {
			http.Error(w, "user already exists, pick another name", http.StatusConflict)
			return
		}
	}

	// Check to see if someone else has already logged in with the same
	// name and is an active player. Recall, subscribers and score are the
	// authoratative sources for active users and teams.
	_, nameInUse := gs.subscribers[name]
	_, teamInUse := gs.score[name]
	if nameInUse || teamInUse {
		http.Error(w, "user already exists, pick another name", http.StatusConflict)
		return
	}

	token, err := generateToken(32)
	if err != nil {
		http.Error(w, "could not generate token: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Once the token has been generated, we save it in our token2user
	// map, but we also save the name and team in our pending list. This
	// prevents a race condition where two users could enter the login
	// requesting the same name, but before either has transitioned to an
	// active user in the subscribe handler.
	gs.token2user[token] = user{name, team}
	gs.pending[name] = struct{}{}
	gs.pending[team] = struct{}{}

	http.SetCookie(w, &http.Cookie{Name: "host", Value: strconv.FormatBool(team == gs.hostTeamName)})
	http.SetCookie(w, &http.Cookie{Name: "token", Value: token})
	http.Redirect(w, r, "/subscriber.html", http.StatusSeeOther)
}

// subscribeHandler processes incoming websocket requests, adding the user to
// the game, setting up the subscriber loop to process incoming/outgoing
// messages, and finally removing the user and closing the websocket.
func (gs *gameShow) subscribeHandler(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		gs.logf("%v", err)
		return
	}
	defer c.Close(websocket.StatusInternalError, "")

	cookie, err := r.Cookie("token")
	if err != nil {
		c.Close(websocket.StatusInternalError, "cookie 'token' not found")
		return
	}

	s, err := gs.addSubscriber(c, cookie.Value)
	if err != nil {
		c.Close(websocket.StatusInternalError, err.Error())
		return
	}

	if !gs.isHost(s) {
		gs.publish(joinMessage{s.Name, s.Team})
	}

	// Main loop for the subcriber, only exits when connection closed.
	err = gs.subscriberLoop(r.Context(), s)

	gs.deleteSubscriber(s)
	if !gs.isHost(s) {
		gs.publish(leaveMessage{s.Name, s.Team})
	}

	if errors.Is(err, context.Canceled) {
		c.Close(websocket.StatusInternalError, err.Error())
		return
	}
	if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
		websocket.CloseStatus(err) == websocket.StatusGoingAway {
		return
	}
	if err != nil {
		gs.logf("%v", err)
		c.Close(websocket.StatusInternalError, err.Error())
		return
	}
}

// addSubscriber updates the game state with the user and team, queues an
// outgoing message with current game state, and removes the name and team
// from the pending map now that they have been added to the game state.
// Returns the subcriber data structure for the user on this websocket.
func (gs *gameShow) addSubscriber(c *websocket.Conn, token string) (*subscriber, error) {
	gs.gameStateMu.Lock()
	defer gs.gameStateMu.Unlock()

	user, tokenFound := gs.token2user[token]
	if !tokenFound {
		return nil, fmt.Errorf("token not found")
	}
	if _, alreadyLoggedIn := gs.subscribers[user.Name]; alreadyLoggedIn {
		return nil, fmt.Errorf("duplicate login attempt for %s", user.Name)
	}

	gs.logf("Adding subcriber %s to %s", user.Name, user.Team)
	s := &subscriber{
		user:     user,
		conn:     c,
		errc:     make(chan error, 1),
		incoming: make(chan any, gs.subscriberBuffer),
		outgoing: make(chan any, gs.subscriberBuffer),
	}

	// First message to our subscriber is the current game state, so the
	// UI can show existing users, team scores, and current buzz state.
	// This state does not include this new subscriber as one of the users
	// because we'll publish a joinMessage to everyone as part of the
	// subscriber loop.
	s.outgoing <- encodeMessage(gs.buildGameStateMessage())

	// Add the name and team to the respective game state maps.
	gs.subscribers[s.Name] = s
	_, teamExists := gs.score[s.Team]
	if !gs.isHost(s) && !teamExists {
		gs.score[s.Team] = 0
	}

	// Remove name and team from pending as they have now been added.
	delete(gs.pending, s.Name)
	delete(gs.pending, s.Team)

	return s, nil
}

// subscriberLoop is the main event loop that handles incoming and outgoing
// messages to/from a subscriber.
func (gs *gameShow) subscriberLoop(ctx context.Context, s *subscriber) error {
	go gs.queueIncomingMessages(ctx, s)

	for {
		select {
		case msg := <-s.incoming:
			gs.logf("received message from %s: %#v", s.Name, msg)
			switch msg := msg.(type) {
			case chatMessage:
				gs.publish(msg)
			case buzzerMessage:
				gs.buzzedIn(s)
			case scoreChangeMessage:
				if !gs.isHost(s) {
					gs.logf("%s not permitted to send score change messages", s.Name)
					continue
				}
				gs.scoreChanged(s, msg)
			case resetBuzzerMessage:
				if !gs.isHost(s) {
					gs.logf("%s not permitted to send reset buzzer messages", s.Name)
					continue
				}
				gs.clearBuzzedIn(s)
			default:
				gs.logf("received unknown message from %s: %#v", s.Name, msg)
			}
		case msg := <-s.outgoing:
			gs.logf("writing msg to %s: %#v", s.Name, msg)
			err := writeTimeout(ctx, time.Second*5, s.conn, msg)
			if err != nil {
				return err
			}
		case err := <-s.errc:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// queueIncomingMessages reads incoming messages from the websocket and sends
// them on a channel to be processed by the subscriberLoop.
func (gs *gameShow) queueIncomingMessages(ctx context.Context, s *subscriber) {
	defer close(s.errc)
	defer close(s.incoming)

	for {
		var msg rawMessage
		err := wsjson.Read(ctx, s.conn, &msg)
		if err != nil {
			s.errc <- err
			break
		}
		if ctx.Err() != nil {
			s.errc <- ctx.Err()
			break
		}
		decoded, err := decodeMessage(msg)
		if err != nil {
			s.errc <- err
			break
		}
		s.incoming <- decoded
	}
}

// publish encodes a message as JSON and then places it on all subscribers'
// outgoing channel. This method will never block. Websockecs of subscribers
// that cannot keep up are closed.
func (gs *gameShow) publish(msg any) {
	gs.gameStateMu.Lock()
	defer gs.gameStateMu.Unlock()

	gs.publishLimiter.Wait(context.Background())

	for _, s := range gs.subscribers {
		select {
		case s.outgoing <- encodeMessage(msg):
		default:
			go s.closeSlow()
		}
	}
}

// buildGameStateMessage returns a gameStateMessage the represents the current
// state of the game, which consists of users, team score, and users currently
// buzzed in. Note: this method must only be called when the lock on game
// state has been acquired.
func (gs *gameShow) buildGameStateMessage() gameStateMessage {
	// Note: even though we queue this msg for sending, we need to realize
	// it may not be sent until some point in the future, so it cannot
	// have references to our gamestate which must be done via locks.

	// First, send game state to all new subs
	// need to initialize for JSON
	users := []user{}
	for _, u := range gs.subscribers {
		if !gs.isHost(u) {
			users = append(users, user{u.Name, u.Team})
		}
	}

	// Make a copy of gs.buzzed and gs.score at this point in time while
	// we have the lock.  If we created a message with references to the
	// gs.buzzed and gs.score, they might change before the msg is
	// actually sent to the subscriber.
	buzzed := make([]string, len(gs.buzzed))
	copy(buzzed, gs.buzzed)

	score := make(map[string]int, len(gs.score))
	for k, v := range gs.score {
		score[k] = v
	}
	msg := gameStateMessage{
		Users:  users,
		Buzzed: buzzed,
		Score:  score,
	}
	return msg
}

// deleteSubscriber removes the specified subscriber from the game state. This
// includes removing the team if the subscriber was the last member. It also
// removes the subscriber from the buzzed-in list. This method does not close
// the websocket.
func (gs *gameShow) deleteSubscriber(s *subscriber) {
	gs.gameStateMu.Lock()
	defer gs.gameStateMu.Unlock()

	gs.logf("Removing subcriber %s", s.Name)
	delete(gs.subscribers, s.Name)

	// Remove team from scoreboard if no one left on team
	remainingUsersInTeam := 0
	for _, o := range gs.subscribers {
		if o.Team == s.Team {
			remainingUsersInTeam += 1
		}
	}
	if remainingUsersInTeam == 0 {
		delete(gs.score, s.Team)
	}

	// Remove from buzzed as well
	newBuzzed := []string{}
	for _, n := range gs.buzzed {
		if n != s.Name {
			newBuzzed = append(newBuzzed, n)
		}
	}
	gs.buzzed = newBuzzed
}

// scoreChanged updates the scoreboard in response to a scoreChangeMessage and
// then publishes the event to all subscirbers.
func (gs *gameShow) scoreChanged(s *subscriber, msg scoreChangeMessage) {
	gs.logf("%s changed score for team %s to %d", s.Name, msg.Team, msg.Score)

	gs.gameStateMu.Lock()
	gs.score[msg.Team] = msg.Score
	gs.gameStateMu.Unlock()

	gs.publish(msg)
}

// buzzedIn updates the list of users that have buzzed in and then publishes
// the event to all subscribers. If a subscriber has already buzzed in, then
// this is a no-op.
func (gs *gameShow) buzzedIn(s *subscriber) {
	gs.logf("%s buzzed in", s.Name)

	gs.gameStateMu.Lock()
	alreadyBuzzed := slices.Contains(gs.buzzed, s.Name)
	if !alreadyBuzzed {
		gs.buzzed = append(gs.buzzed, s.Name)
	}
	gs.gameStateMu.Unlock()

	if !alreadyBuzzed {
		gs.publish(buzzerMessage{s.Name})
	}
}

// clearBuzzedIn removes all subscribers from the buzzed in list and then
// publishes a resetBuzzerMessage to all subscribers.
func (gs *gameShow) clearBuzzedIn(s *subscriber) {
	gs.logf("%s reset the buzzer", s.Name)

	gs.gameStateMu.Lock()
	gs.buzzed = gs.buzzed[:0]
	gs.gameStateMu.Unlock()

	gs.publish(resetBuzzerMessage{})
}

// writeTimeout sends a JSON message to the websocket connection with the
// specified timeout value.
func writeTimeout(ctx context.Context, timeout time.Duration, c *websocket.Conn, msg any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return wsjson.Write(ctx, c, msg)
}

// generateToken creates a unique token of length bits.
func generateToken(length int) (string, error) {
	b := make([]byte, length)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
