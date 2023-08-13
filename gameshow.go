package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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

// gameShow enables receiving and broadcasting Messages
// between subscribers of the game show.
type gameShow struct {
	// subscriberMsgBuffer controls the max number
	// of messages that can be queued for a subscriber
	// before it is kicked.
	//
	// Defaults to 16.
	subscriberMsgBuffer int

	// publishLimiter controls the rate limit applied
	// to the subcribers publishing messages.
	//
	// Defaults to one publish every 100ms with a burst of 20.
	publishLimiter *rate.Limiter

	// logf controls where logs are sent.
	//
	// Defaults to log.Printf.
	logf func(f string, v ...interface{})

	// serveMux routes the various endpoints
	// to the appropriate handler.
	serveMux http.ServeMux

	// Team name that hosts use to join the game
	hostTeam string

	// game state mutex protects resources below
	gameStateMu sync.Mutex

	buzzed      []string               // names that buzzed in order
	score       map[string]int         // key: team
	token2user  map[string]User        // key: token value: name
	subscribers map[string]*subscriber // key: name
	pending     map[string]struct{}    // key: name or team
}

// newGameShow constructs a gameShow with the defaults.
func newGameShow(hostTeam string) *gameShow {
	gs := &gameShow{
		subscriberMsgBuffer: 32,
		logf:                log.Printf,
		hostTeam:            hostTeam,
		buzzed:              []string{}, // not nil for JSON serialization
		score:               make(map[string]int),
		token2user:          make(map[string]User),
		subscribers:         make(map[string]*subscriber),
		pending:             make(map[string]struct{}),
		publishLimiter:      rate.NewLimiter(rate.Every(time.Millisecond*1), 100),
	}
	gs.serveMux.Handle("/", http.FileServer(http.Dir(".")))
	gs.serveMux.HandleFunc("/login", gs.loginHandler)
	gs.serveMux.HandleFunc("/join", gs.subscribeHandler)

	return gs
}

type User struct {
	Name string `json:"name"`
	Team string `json:"team"`
}

// subscriber represents a subscriber.
// Messages are sent on the msgs channel and if the client
// cannot keep up with the messages, closeSlow is called.
type subscriber struct {
	User
	errc     chan error
	incoming chan any
	outgoing chan any
	conn     *websocket.Conn
}

func (s *subscriber) closeSlow() {
	s.conn.Close(websocket.StatusPolicyViolation, "connection too slow to keep up with messages")
}

func (gs *gameShow) isHost(s *subscriber) bool {
	return s.Team == gs.hostTeam
}

func (gs *gameShow) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	gs.serveMux.ServeHTTP(w, r)
}

func (gs *gameShow) loginHandler(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	name := query.Get("name")
	team := query.Get("team")

	gs.gameStateMu.Lock()
	defer gs.gameStateMu.Unlock()

	if name == team {
		http.Error(w, "name and team must be different, pick another name", http.StatusConflict)
		return
	}

	for pendingNameOrTeam := range gs.pending {
		if pendingNameOrTeam == name {
			http.Error(w, "user already exists, pick another name", http.StatusConflict)
			return
		}
	}

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

	gs.logf("reserving %s with token %s", name, token)
	gs.token2user[token] = User{name, team}

	gs.pending[name] = struct{}{}
	gs.pending[team] = struct{}{}

	http.SetCookie(w, &http.Cookie{Name: "host", Value: strconv.FormatBool(team == gs.hostTeam)})
	http.SetCookie(w, &http.Cookie{Name: "token", Value: token})
	http.Redirect(w, r, "/subscriber.html", http.StatusSeeOther)
}

// subscribeHandler accepts the WebSocket connection
// and then processes messages sent from the subscriber
// as well as broadcasting messages to the subscriber.
func (gs *gameShow) subscribeHandler(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		gs.logf("%v", err)
		return
	}
	defer c.Close(websocket.StatusInternalError, "")

	// token that we provided when they "login"
	token, err := r.Cookie("token")
	if err != nil {
		gs.logf("token cookie not found")
		c.Close(websocket.StatusInternalError, "token cookie not found")
		return
	}

	gs.gameStateMu.Lock()
	user, ok := gs.token2user[token.Value]
	_, alreadyLoggedIn := gs.subscribers[user.Name]
	gs.gameStateMu.Unlock()

	if !ok {
		gs.logf("token not valid: %s", token.Value)
		c.Close(websocket.StatusInternalError, "token not valid")
		return
	}

	if alreadyLoggedIn {
		gs.logf("%s trying to join again, closing websocket", user.Name)
		c.Close(websocket.StatusInternalError, "duplicate login attempt")
		return
	}

	s := &subscriber{
		User:     user,
		conn:     c,
		errc:     make(chan error, 1),
		incoming: make(chan any, gs.subscriberMsgBuffer),
		outgoing: make(chan any, gs.subscriberMsgBuffer),
	}

	err = gs.subscriberLoop(r.Context(), s)

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

func (gs *gameShow) subscriberLoop(ctx context.Context, s *subscriber) error {
	gs.addSubscriber(s)
	defer gs.deleteSubscriber(s)

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
				if s.Team != gs.hostTeam {
					gs.logf("%s not permitted to send score change messages", s.Name)
					continue
				}
				gs.scoreChanged(s, msg)
			case resetBuzzerMessage:
				if s.Team != gs.hostTeam {
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

// publish publishes the msg to all subscribers.
// It never blocks and so messages to slow subscribers
// are dropped.
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

// addSubscriber registers a subscriber.
func (gs *gameShow) addSubscriber(s *subscriber) {
	gs.logf("Adding subcriber %s", s.Name)

	gs.gameStateMu.Lock()

	// First, send game state to all new subs
	users := []User{} // need to initialize for JSON
	for _, u := range gs.subscribers {
		if !gs.isHost(u) {
			users = append(users, User{u.Name, u.Team})
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

	// Note: even though we queue this msg for sending, we need to realize
	// it may not be sent until some point in the future, so it cannot
	// have references to our gamestate which must be done via locks.
	s.outgoing <- encodeMessage(msg)

	// Then add user to subscribers list after we send state as we'll
	// send separate join message at end of this method.
	gs.subscribers[s.Name] = s

	// Check to see if team exists and isn't a host team, add to scroreboard.
	_, teamExists := gs.score[s.Team]
	if !gs.isHost(s) && !teamExists {
		gs.score[s.Team] = 0
	}

	// Now that we've added the name and team to gs.subscribers and
	// gs.score, we can remove them from pending.
	delete(gs.pending, s.Name)
	delete(gs.pending, s.Team)

	gs.gameStateMu.Unlock()

	// Finally, notify others unless it is a host.
	if !gs.isHost(s) {
		gs.publish(joinMessage{s.Name, s.Team})
	}
}

// deleteSubscriber deletes the given subscriber.
func (gs *gameShow) deleteSubscriber(s *subscriber) {
	gs.logf("Removing subcriber %s", s.Name)

	gs.gameStateMu.Lock()
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
	gs.gameStateMu.Unlock()

	if !gs.isHost(s) {
		gs.publish(leaveMessage{s.Name, s.Team})
	}
}

func (gs *gameShow) scoreChanged(s *subscriber, msg scoreChangeMessage) {
	gs.logf("%s changed score for team %s to %d", s.Name, msg.Team, msg.Score)

	gs.gameStateMu.Lock()
	gs.score[msg.Team] = msg.Score
	gs.gameStateMu.Unlock()

	gs.publish(msg)
}

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

func (gs *gameShow) clearBuzzedIn(s *subscriber) {
	gs.logf("%s reset the buzzer", s.Name)

	gs.gameStateMu.Lock()
	gs.buzzed = gs.buzzed[:0]
	gs.gameStateMu.Unlock()

	gs.publish(resetBuzzerMessage{})
}

func writeTimeout(ctx context.Context, timeout time.Duration, c *websocket.Conn, msg any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return wsjson.Write(ctx, c, msg)
}

func generateToken(length int) (string, error) {
	b := make([]byte, length)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
