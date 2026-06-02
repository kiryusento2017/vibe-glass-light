package watcher

import (
	"testing"
	"claude-traffic-light/state"
)

func TestParseLastState(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  state.State
	}{
		{"empty", nil, state.Green},
		{
			"user message → idle",
			[]string{`{"type":"user","message":{"role":"user","content":"hi"}}`},
			state.Green,
		},
		{
			"assistant text → thinking",
			[]string{
				`{"type":"user","message":{"role":"user","content":"hi"}}`,
				`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"sure"}]}}`,
			},
			state.Yellow,
		},
		{
			"tool_use → running",
			[]string{
				`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"1","name":"Bash","input":{}}]}}`,
			},
			state.Red,
		},
		{
			"tool_result after tool_use → idle",
			[]string{
				`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"1","name":"Bash","input":{}}]}}`,
				`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"1","content":"ok"}]}}`,
			},
			state.Green,
		},
		{
			"malformed json → safe default green",
			[]string{`not json`},
			state.Green,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseLastState(c.lines)
			if got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
