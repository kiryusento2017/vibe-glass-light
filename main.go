package main

import (
	"os"
	"path/filepath"
	"time"

	"claude-traffic-light/config"
	"claude-traffic-light/state"
	"claude-traffic-light/ui"
	"claude-traffic-light/watcher"
)

func main() {
	exePath, _ := os.Executable()
	cfgPath := filepath.Join(filepath.Dir(exePath), "config.json")
	cfg, _ := config.Load(cfgPath)

	win := ui.New(cfgPath, cfg)

	w, err := watcher.New(
		watcher.ClaudeProjectsPath(),
		60*time.Second,
		func(s state.State) { win.SetState(s) },
	)
	if err == nil {
		go w.Watch()
		defer w.Stop()
	}

	win.Run()
}
