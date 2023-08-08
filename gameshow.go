package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"golang.org/x/time/rate"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// gameShow enables receiving and broadcasting Messages
// between subscribers of the game show.
type gameShow struct {
	// subscriberMessageBuffer controls the max number
	// of messages that can be queued for a subscriber
	// before it is kicked.
	//
	// Defaults to 16.
	subscriberMessageBuffer int

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

	buzzed      map[string]int         // key: name value: order they buzzed in
	score       map[string]int         // key: team
	token2user  map[string]User        // key: token value: name
	subscribers map[string]*subscriber // key: name
}

// newGameShow constructs a gameShow with the defaults.
func newGameShow(hostTeam string) *gameShow {
	gs := &gameShow{
		subscriberMessageBuffer: 16,
		logf:                    log.Printf,
		hostTeam:                hostTeam,
		buzzed:                  make(map[string]int),
		score:                   make(map[string]int),
		token2user:              make(map[string]User),
		subscribers:             make(map[string]*subscriber),
		publishLimiter:          rate.NewLimiter(rate.Every(time.Millisecond*50), 50),
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

	if _, userExists := gs.subscribers[name]; userExists {
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

	params := url.Values{}
	params.Add("token", token)
	if team == gs.hostTeam {
		params.Add("isHost", "true")
	}
	http.Redirect(w, r, "/subscriber.html?"+params.Encode(), http.StatusSeeOther)
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
	token := r.URL.Query().Get("token")

	// move user from pending to subscribers, capture the websocket
	// in our subcriber struct, so it's safe to fully initialized
	// and ready for the subscriber loop.
	gs.gameStateMu.Lock()
	user, ok := gs.token2user[token]
	if !ok {
		gs.gameStateMu.Unlock()
		gs.logf("token not found: %s", token)
		c.Close(websocket.StatusInternalError, "token not valid")
		return
	}

	s := &subscriber{
		User:     user,
		errc:     make(chan error, 1),
		conn:     c,
		incoming: make(chan any, 1),
		outgoing: make(chan any, gs.subscriberMessageBuffer),
	}
	gs.gameStateMu.Unlock()

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
			case resetBuzzerMessage:
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
	close(s.errc)
	close(s.incoming)
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

	msg := gameStateMessage{
		Users:  users,
		Buzzed: gs.buzzed,
		Score:  gs.score,
	}

	s.outgoing <- encodeMessage(msg)

	// Then add user to subscribers list after we send state as we'll
	// send separate join message at end of this method.
	gs.subscribers[s.Name] = s

	// Check to see if they had buzzed in already. How can that be if it's
	// a new addSubscriber? They might have been disconnected and have
	// reconnected, so let's find out if they buzzed in. If not, buzz will
	// be set to 0 (default value of int type).
	buzz := gs.buzzed[s.Name]
	gs.gameStateMu.Unlock()

	// Finally, notify others unless it is a host.
	if !gs.isHost(s) {
		gs.publish(joinMessage{s.Name, s.Team, buzz})
	}
}

// deleteSubscriber deletes the given subscriber.
func (gs *gameShow) deleteSubscriber(s *subscriber) {
	gs.logf("Removing subcriber %s", s.Name)

	gs.gameStateMu.Lock()
	delete(gs.subscribers, s.Name)
	gs.gameStateMu.Unlock()

	if !gs.isHost(s) {
		gs.publish(leaveMessage{s.Name, s.Team})
	}
}

func (gs *gameShow) buzzedIn(s *subscriber) {
	gs.logf("%s buzzed in", s.Name)

	gs.gameStateMu.Lock()
	_, alreadyBuzzed := gs.buzzed[s.Name]
	if !alreadyBuzzed {
		gs.buzzed[s.Name] = len(gs.buzzed) + 1
	}
	gs.gameStateMu.Unlock()

	if !alreadyBuzzed {
		gs.publish(buzzerMessage{s.Name, gs.buzzed[s.Name]})
	}
}

func (gs *gameShow) clearBuzzedIn(s *subscriber) {
	gs.logf("%s reset the buzzer", s.Name)

	gs.gameStateMu.Lock()
	clear(gs.buzzed)
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
