package main

import (
	"context"
	"errors"
	"log"
	"net/http"
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

	subscribersMu sync.Mutex
	subscribers   map[*subscriber]struct{}
}

// newGameShow constructs a gameShow with the defaults.
func newGameShow() *gameShow {
	gs := &gameShow{
		subscriberMessageBuffer: 16,
		logf:                    log.Printf,
		subscribers:             make(map[*subscriber]struct{}),
		publishLimiter:          rate.NewLimiter(rate.Every(time.Millisecond*100), 20),
	}
	gs.serveMux.Handle("/", http.FileServer(http.Dir(".")))
	gs.serveMux.HandleFunc("/join", gs.subscribeHandler)

	return gs
}

// subscriber represents a subscriber.
// Messages are sent on the msgs channel and if the client
// cannot keep up with the messages, closeSlow is called.
type subscriber struct {
	name      string
	team      string
	errc      chan error
	incoming  chan any
	outgoing  chan any
	closeSlow func()
}

func (s subscriber) isHost() bool {
	return s.team == "host"
}

func (gs *gameShow) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	gs.serveMux.ServeHTTP(w, r)
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

	query := r.URL.Query()
	name := query.Get("name")
	team := query.Get("team")

	err = gs.subscriberLoop(r.Context(), name, team, c)
	if errors.Is(err, context.Canceled) {
		return
	}
	if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
		websocket.CloseStatus(err) == websocket.StatusGoingAway {
		return
	}
	if err != nil {
		gs.logf("%v", err)
		return
	}
}

// TODO: update comment
//
// subscriberLoop subscribes the given WebSocket to all broadcast messages.
// It creates a subscriber with a buffered msgs chan to give some room to slower
// connections and then registers the subscriber. It then listens for all messages
// and writes them to the WebSocket. If the context is cancelled or
// an error occurs, it returns and deletes the subscription.
func (gs *gameShow) subscriberLoop(ctx context.Context, name, team string, c *websocket.Conn) error {
	s := &subscriber{
		name:     name,
		team:     team,
		errc:     make(chan error, 1),
		incoming: make(chan any, 1),
		outgoing: make(chan any, gs.subscriberMessageBuffer),
		closeSlow: func() {
			c.Close(websocket.StatusPolicyViolation, "connection too slow to keep up with messages")
		},
	}
	gs.addSubscriber(s)
	defer gs.deleteSubscriber(s)

	go gs.queueIncomingMessages(ctx, c, s)

	for {
		select {
		case msg := <-s.outgoing:
			err := writeTimeout(ctx, time.Second*5, c, msg)
			if err != nil {
				return err
			}
		case msg := <-s.incoming:
			switch msg := msg.(type) {
			case chatMessage:
				gs.publish(msg)
			default:
				gs.logf("received unknown message type: %v", msg)
			}
		case err := <-s.errc:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (gs *gameShow) queueIncomingMessages(ctx context.Context, c *websocket.Conn, s *subscriber) {
	for {
		var msg rawMessage
		err := wsjson.Read(ctx, c, &msg)
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
func (cs *gameShow) publish(msg any) {
	cs.subscribersMu.Lock()
	defer cs.subscribersMu.Unlock()

	cs.publishLimiter.Wait(context.Background())

	for s := range cs.subscribers {
		select {
		case s.outgoing <- msg:
		default:
			go s.closeSlow()
		}
	}
}

// addSubscriber registers a subscriber.
func (gs *gameShow) addSubscriber(s *subscriber) {
	gs.logf("Adding subcriber ...")
	gs.subscribersMu.Lock()
	gs.subscribers[s] = struct{}{}
	gs.subscribersMu.Unlock()

	// Upon connection, notify others unless it is a host.
	if !s.isHost() {
		gs.publish(encodeMessage(joinMessage{s.name, s.team}))
	}
}

// deleteSubscriber deletes the given subscriber.
func (gs *gameShow) deleteSubscriber(s *subscriber) {
	gs.logf("Removing subcriber ...")
	gs.subscribersMu.Lock()
	delete(gs.subscribers, s)
	gs.subscribersMu.Unlock()

	if !s.isHost() {
		gs.publish(encodeMessage(leaveMessage{s.name, s.team}))
	}
}

func writeTimeout(ctx context.Context, timeout time.Duration, c *websocket.Conn, msg any) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return wsjson.Write(ctx, c, msg)
}
