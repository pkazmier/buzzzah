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
// that is intended to be broadcast to all other subscribers (participants or
// hosts). Name is the sender and Text is the message to broadcast.
//
// The game server will publish this chatMessage to all subscribers with an
// envelope type set to "chat". Upon receipt, game clients should display this
// message to its users.
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
// or hosts) with an envelope type set to "join". Upon receipt, game clients
// must update their state to reflect a new participant has joined a team,
// which may or may not exist yet from the client's perspective.
//
// Clients do not send joinMessages and it is an error to do so.
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
// (not host) upon successful login to the server. The client's UI should not
// be updated to reflect the local user joining the game until receipt of this
// message.
type joinMessage struct {
	Name string `json:"name"`
	Team string `json:"team"`
}

// leaveMessage represents a message sent by the game server when a
// participant has left the game. Name is the participant's name and Team was
// the team they were on.
//
// # The game server will
//
// leaveMessage not sent for hosts.
//
// Clients never send leave messages. Only the server.
//
// Clients should remove teams with 0 participants.
//
// message.type == "leave"
type leaveMessage struct {
	Name string `json:"name"`
	Team string `json:"team"`
}

// buzzerMessage sent by subscriber to "buzz in".
//
// message.type == "buzzer"
type buzzerMessage struct {
	Name string `json:"name"`
}

// resetBuzzerMessage sent by host to reset those who buzzed in.
//
// message.type == "reset"
type resetBuzzerMessage struct{}

// scoreChangeMessag send by host to update one team's score.
type scoreChangeMessage struct {
	Team  string `json:"team"`
	Score int    `json:"score"`
}

// gameStateMessage sent by host to update subscribers' sccoreboards.
//
// message.type == "state"
type gameStateMessage struct {
	Users  []user         `json:"users"`
	Buzzed []string       `json:"buzzed"`
	Score  map[string]int `json:"scoreboard"`
}

// will panic if we try to encode a non-existent message type
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
