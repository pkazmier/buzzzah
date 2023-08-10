package main

import (
	"encoding/json"
	"fmt"
)

// TODO: Use omitifempty for all optional fields

type rawMessage struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type message struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

// chatMessage sent by subscriber to broadcast a chat.
//
// message.type == "chat"
type chatMessage struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// joinMessage sent by server when subscriber joins.
//
// message.type == "join"
type joinMessage struct {
	Name string `json:"name"`
	Team string `json:"team"`
	Buzz int    `json:"buzz"`
}

// leaveMessage sent by server when subscriber leaves.
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
	Buzz int    `json:"buzz"`
}

// resetBuzzerMessage sent by host to reset those who buzzed in.
//
// message.type == "reset"
type resetBuzzerMessage struct{}

// scoreChangeMessag send by host to update one team's score.
type scoreChangeMessage struct {
	Team  string `json:"team"`
	Score int    `json:"score"`
	// Prior int    `json:"prior"`
}

// gameStateMessage sent by host to update subscribers' sccoreboards.
//
// message.type == "state"
type gameStateMessage struct {
	Users  []User         `json:"users"`
	Buzzed map[string]int `json:"buzzed"`
	Score  map[string]int `json:"scoreboard"`
}

// will panic if we try to encode a non-existent message type
func encodeMessage(msg any) message {
	var encoded message
	switch msg.(type) {
	case chatMessage:
		encoded = message{Type: "chat", Data: msg}
	case joinMessage:
		encoded = message{Type: "join", Data: msg}
	case leaveMessage:
		encoded = message{Type: "leave", Data: msg}
	case buzzerMessage:
		encoded = message{Type: "buzzer", Data: msg}
	case resetBuzzerMessage:
		encoded = message{Type: "reset", Data: msg}
	case scoreChangeMessage:
		encoded = message{Type: "score", Data: msg}
	case gameStateMessage:
		encoded = message{Type: "state", Data: msg}
	}
	return encoded
}

func decodeMessage(msg rawMessage) (any, error) {
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
