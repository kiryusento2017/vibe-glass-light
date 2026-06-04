package watcher

import (
	"os"
	"strings"
	"time"

	"claude-traffic-light/state"
)

// Watcher 轮询 hook 写的状态文件，内容变化时回调。
// 数据源是 Claude Code hook（PreToolUse/PostToolUse/UserPromptSubmit/Stop）
// 实时写入的状态词，比解析 transcript 实时、准确。常亮：保持最后状态，不超时。
type Watcher struct {
	statePath string
	onChange  func(state.State)
	stop      chan struct{}
	last      state.State
}

// New 创建状态文件监测器。statePath 是 hook 写、挂件读的状态文件路径。
func New(statePath string, onChange func(state.State)) *Watcher {
	return &Watcher{
		statePath: statePath,
		onChange:  onChange,
		stop:      make(chan struct{}),
		last:      state.Grey,
	}
}

func (w *Watcher) Stop() { close(w.stop) }

// Watch 每 100ms 读状态文件，内容变化时回调。阻塞 — 在 goroutine 里调。
func (w *Watcher) Watch() {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	w.poll()
	for {
		select {
		case <-w.stop:
			return
		case <-tick.C:
			w.poll()
		}
	}
}

func (w *Watcher) poll() {
	s := w.read()
	if s != w.last {
		w.last = s
		w.onChange(s)
	}
}

// read 读状态文件并映射为四态。文件不存在=还没任何活动=灰；
// 否则保持最后写入的状态（常亮，不超时变灰）。
func (w *Watcher) read() state.State {
	data, err := os.ReadFile(w.statePath)
	if err != nil {
		return state.Grey
	}
	switch strings.TrimSpace(string(data)) {
	case "running":
		return state.Red
	case "thinking":
		return state.Yellow
	case "idle":
		return state.Green
	default:
		return state.Grey
	}
}
