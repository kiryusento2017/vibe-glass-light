package main

import (
	"os"
	"path/filepath"

	"claude-traffic-light/config"
	"claude-traffic-light/ui"
)

func main() {
	exePath, _ := os.Executable()
	cfgPath := filepath.Join(filepath.Dir(exePath), "config.json")
	cfg, _ := config.Load(cfgPath)
	win := ui.New(cfgPath, cfg)
	win.Run()
}
