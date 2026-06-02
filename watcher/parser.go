package watcher

import (
	"encoding/json"
	"claude-traffic-light/state"
)

type transcriptLine struct {
	Type    string   `json:"type"`
	Message *message `json:"message,omitempty"`
}

type message struct {
	Content json.RawMessage `json:"content"`
}

type contentItem struct {
	Type string `json:"type"`
}

// ParseLastState infers state from the last meaningful line of a transcript.
func ParseLastState(lines []string) state.State {
	for i := len(lines) - 1; i >= 0; i-- {
		if lines[i] == "" {
			continue
		}
		var line transcriptLine
		if err := json.Unmarshal([]byte(lines[i]), &line); err != nil {
			continue
		}
		switch line.Type {
		case "user":
			return state.Green
		case "assistant":
			return assistantState(line.Message)
		}
	}
	return state.Green // empty or all-unknown → idle
}

func assistantState(msg *message) state.State {
	if msg == nil {
		return state.Yellow
	}
	var items []contentItem
	if err := json.Unmarshal(msg.Content, &items); err != nil {
		return state.Yellow
	}
	for _, item := range items {
		if item.Type == "tool_use" {
			return state.Red
		}
	}
	return state.Yellow
}
