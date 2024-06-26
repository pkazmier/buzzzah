package main

import (
	"encoding/json"
	"fmt"
)

// TODO: Use omitifempty for all optional fields

// user represents the name of the user and the team the user is on. Both user
// names and team names share the same namespace, so a user name must never be
// the same as an existing team name.
type user struct {
	Name string `json:"name"`
	Team string `json:"team"`
}

// incomingMessageEnvelope represents an incoming envelope from a subscriber
// (participant or host), which includes the type of the message and the
// undecoded JSON message. The type of message is used decode the data into a
// type-specific message.
type incomingMessageEnvelope struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// outgoingMessageEnvelope represents an outgoing envelope to be sent to a
// subscriber (participant or host), which includes the type of the message
// and the message itself. The type of message is used to encode the data.
type outgoingMessageEnvelope struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// chatMessage represents a message sent by a subscriber (participant or host)
// that is intended to be broadcast to all other subscribers. Name is the
// sender and Text is the message to broadcast.
//
// Upon receipt of a chatMessage, the game server will publish it to all
// subscribers (participants and hosts) including the sender with an envelope
// type set to "chat". Game clients should display this message to its users
// upon receipt.
//
// On a client, it is not an error condition to receive a chatMessage from a
// non-participant in the game. For example, hosts can send chat messages, so
// clients must accept messages from all senders regardless of whether or not
// it has seen Name referenced in a joinMessage or gameStateMessage.
type chatMessage struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// joinMessage represents a message sent by the game server when a new
// participant has joined the game. Name is the participant's name and Team is
// the team joined.
//
// The game server will publish a joinMessage to all subscribers (participants
// and hosts) with an envelope type set to "join". Upon receipt, game clients
// must update their state to reflect a new participant has joined a team. If
// the team doesn't exist yet, the client must add it as well.
//
// Clients should not send joinMessages for their local users. It is an error
// to do so. The game server will publish joinMessage to other subscribers on
// behalf of the client upon subscription.
//
// Clients are guaranteed that participants will never have the same name as a
// team because this is a shared namespace. This invariant is enforced by the
// server.
//
// The game server does not send joinMessages when a host joins a game. Join
// messages are reserved for participants only. Hosts are invisible to clients
// with one exception: they can send chatMessages.
//
// Game clients, however, will receive a joinMessage for a local participant
// (not host) upon successful login to the server. The client UI should not
// reflect the local user joining the game until receipt of this message.
type joinMessage struct {
	Name string `json:"name"`
	Team string `json:"team"`
}

// leaveMessage represents a message sent by the game server when an existing
// participant has left the game. Name is the participant leaving and Team is
// the team they were on.
//
// The game server will publish leaveMessage to all subscribers (participants
// or hosts) with an envelope type set to "leave". Upon receipt, game clients
// must update their state to reflect a participant has left the game. If the
// participant was the last member of a team, the client must remove the team
// as well.
//
// Clients should not send leaveMessages for their local users. It is an error
// to do so. The game server will publish leaveMessage to other subscribers on
// behalf of a client who has left the game.
//
// The game server does not send leaveMessages when a host leaves a game.
// Leave messages are reserved for participants only. Hosts are invisible to
// clients with one exception: they can send chatMessages.
type leaveMessage struct {
	Name string `json:"name"`
	Team string `json:"team"`
}

// buzzerMessage represents a message sent by a participant, and broadcast to
// all subscribers by the game server, to indicate a participant has "buzzed
// in". Name is the participant that sent the message.
//
// When the game server receives a buzzerMessage, it publishes the message to
// all subscribers (participants and hosts), including the sender, with an
// envelope type set to "buzzer".  Upon receipt game clients must update their
// UI to reflect a participant has buzzed in--including the local user. It is
// the responsibility of the client to distinguish the order participants
// buzzed in.
//
// The game server guarantees that a user may only buzz in once. Upon receipt
// of multiple messages from a client, the server will simply discard all but
// the first until it receives a resetBuzzerMessage from a host.
type buzzerMessage struct {
	Name string `json:"name"`
}

// resetBuzzerMessage represents a message sent by a host to reset the game
// server's buzzer state and to notify clients the buzzers have been reset.
// Only hosts are allowed to send this message.
//
// Upon receipt of a resetBuzzerMessage, the game server will publish it to
// all subscribers (participants and hosts) including the sender with an
// envelope type set to "reset".  Game clients should clear any indication of
// whether or not a participant has buzzed in upon receipt and ready UI to
// allow buzzing in again.
type resetBuzzerMessage struct{}

// scoreChangeMessage represents a message sent by a hast to update a team's
// score with the message envelope type of "score". Only hosts are allowed to
// send this message.
//
// Upon receipt of a scoreChangeMessage, the game server will publish it to
// all subscribers (participants and hosts). Game clients should update their
// UI to reflect the change in score.
type scoreChangeMessage struct {
	Team  string `json:"team"`
	Score int    `json:"score"`
}

// gameStateMessage represents a message sent by the game server whenever a
// subscriber (participants and hosts) joins. The message is sent with an
// envelope type of "state". Users represents the participants in the game.
// Buzzed is an list of users currently buzzed in ordered by first to last to
// buzz in. Upon receipt of a gameStateMessage, game clients should update
// their UI to reflect the in-progress game.
type gameStateMessage struct {
	Users  []user         `json:"users"`
	Buzzed []string       `json:"buzzed"`
	Score  map[string]int `json:"scoreboard"`
}

// encodeMessage will panic if we try to encode a non-existent message type
// because only we could cause that error.
func encodeMessage(msg any) outgoingMessageEnvelope {
	var encoded outgoingMessageEnvelope
	switch msg.(type) {
	case chatMessage:
		encoded = outgoingMessageEnvelope{Type: "chat", Data: msg}
	case joinMessage:
		encoded = outgoingMessageEnvelope{Type: "join", Data: msg}
	case leaveMessage:
		encoded = outgoingMessageEnvelope{Type: "leave", Data: msg}
	case buzzerMessage:
		encoded = outgoingMessageEnvelope{Type: "buzzer", Data: msg}
	case resetBuzzerMessage:
		encoded = outgoingMessageEnvelope{Type: "reset", Data: msg}
	case scoreChangeMessage:
		encoded = outgoingMessageEnvelope{Type: "score", Data: msg}
	case gameStateMessage:
		encoded = outgoingMessageEnvelope{Type: "state", Data: msg}
	}
	return encoded
}

// decodeMessage will gracefully handle unknown message types from clients and
// log them as we cannot be sure of what a client will send us.
func decodeMessage(msg incomingMessageEnvelope) (any, error) {
	var err error
	switch msg.Type {
	case "chat":
		var decoded chatMessage
		err = json.Unmarshal(msg.Data, &decoded)
		return decoded, err
	case "join":
		var decoded joinMessage
		err = json.Unmarshal(msg.Data, &decoded)
		return decoded, err
	case "leave":
		var decoded leaveMessage
		err = json.Unmarshal(msg.Data, &decoded)
		return decoded, err
	case "buzzer":
		var decoded buzzerMessage
		err = json.Unmarshal(msg.Data, &decoded)
		return decoded, err
	case "reset":
		var decoded resetBuzzerMessage
		err = json.Unmarshal(msg.Data, &decoded)
		return decoded, err
	case "score":
		var decoded scoreChangeMessage
		err = json.Unmarshal(msg.Data, &decoded)
		return decoded, err
	case "state": // don't really need this as no one sends us state
		var decoded gameStateMessage
		err = json.Unmarshal(msg.Data, &decoded)
		return decoded, err
	default:
		return nil, fmt.Errorf("received unknown message type: %v", msg.Type)
	}
}
