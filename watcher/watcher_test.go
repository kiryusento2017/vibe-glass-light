package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"claude-traffic-light/state"
)

func TestWatcherReadsRunning(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "agent-light-state")
	os.WriteFile(statePath, []byte("running"), 0644)

	got := make(chan state.State, 5)
	w := New(statePath, func(s state.State) { got <- s })
	go w.Watch()
	defer w.Stop()

	select {
	case s := <-got:
		if s != state.Red {
			t.Errorf("got %v, want Red", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Red")
	}
}

func TestWatcherMapsAllStates(t *testing.T) {
	cases := []struct {
		word string
		want state.State
	}{
		{"running", state.Red},
		{"thinking", state.Yellow},
		{"idle", state.Green},
	}
	for _, c := range cases {
		dir := t.TempDir()
		statePath := filepath.Join(dir, "agent-light-state")
		os.WriteFile(statePath, []byte(c.word+"\n"), 0644)

		got := make(chan state.State, 5)
		w := New(statePath, func(s state.State) { got <- s })
		go w.Watch()

		select {
		case s := <-got:
			if s != c.want {
				t.Errorf("%q: got %v, want %v", c.word, s, c.want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%q: timed out", c.word)
		}
		w.Stop()
	}
}
