package watcher

import (
	"os"
	"path/filepath"
	"testing"
	"time"
	"claude-traffic-light/state"
)

func TestWatcherDetectsChange(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "abc123")
	os.MkdirAll(proj, 0755)

	got := make(chan state.State, 5)
	w, _ := New(root, 5*time.Second, func(s state.State) { got <- s })
	go w.Watch()
	defer w.Stop()

	time.Sleep(100 * time.Millisecond) // let watcher start

	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"1","name":"Bash","input":{}}]}}` + "\n"
	os.WriteFile(filepath.Join(proj, "transcript.jsonl"), []byte(line), 0644)

	select {
	case s := <-got:
		if s != state.Red {
			t.Errorf("got %v, want Red", s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for state change")
	}
}

func TestWatcherIgnoresStaleFiles(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "abc123")
	os.MkdirAll(proj, 0755)

	// 过期会话文件：内容是 tool_use（红），但修改时间设为 10 分钟前
	staleLine := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"1","name":"Bash","input":{}}]}}` + "\n"
	stalePath := filepath.Join(proj, "stale.jsonl")
	os.WriteFile(stalePath, []byte(staleLine), 0644)
	old := time.Now().Add(-10 * time.Minute)
	os.Chtimes(stalePath, old, old)

	got := make(chan state.State, 5)
	w, _ := New(root, 60*time.Second, func(s state.State) { got <- s })
	go w.Watch()
	defer w.Stop()

	// 过期文件应被忽略：不触发任何状态变化（保持初始 Grey，收不到 Red）
	select {
	case s := <-got:
		t.Errorf("stale file should be ignored, but got %v", s)
	case <-time.After(1 * time.Second):
		// 正确：无状态变化
	}
}
