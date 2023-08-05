package main

import (
	"encoding/json"
	"fmt"
)

type message struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
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
}

// leaveMessage sent by server when subscriber leaves.
//
// message.type == "leave"
type leaveMessage struct {
	Name string `json:"name"`
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

// scoreBoardMessage sent by host to update subscribers' sccoreboards.
//
// message.type == "score"
type scoreBoardMessage struct {
	Score map[string]int `json:"scoreboard"`
}

func decodeMessage(msg message) (any, error) {
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
		var decoded scoreBoardMessage
		err = json.Unmarshal(msg.Data, &decoded)
		return decoded, err
	default:
		return nil, fmt.Errorf("received unknown message type: %v", msg.Type)
	}
}
