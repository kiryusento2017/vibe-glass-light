package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// 4 个生命周期 hook → 状态。PreToolUse/PostToolUse 需要 matcher，其余不需要。
var hookEvents = []struct {
	event   string
	state   string
	matcher bool
}{
	{"UserPromptSubmit", "thinking", false},
	{"PreToolUse", "running", true},
	{"PostToolUse", "thinking", true},
	{"Stop", "idle", false},
}

// installHooks 把挂件的 4 个状态 hook 安全合并进 ~/.claude/settings.json。
// 原则：settings.json 必须已存在（即本机已装 Claude Code）才合并；不存在则
// 静默跳过——不在没装 Claude Code 的机器上创建任何目录或文件。
// 已有文件时：幂等（已存在则只更新自己那条的路径，绝不重复加）、只增不删
// （别人的配置一字不动）、先备份（写前存 settings.json.bak）。
func installHooks() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")

	// 读现有 settings。文件不存在 = 本机没装 Claude Code → 静默跳过，不创建。
	orig, err := os.ReadFile(settingsPath)
	if err != nil {
		return
	}
	var root map[string]interface{}
	if json.Unmarshal(orig, &root) != nil {
		return
	}

	hooks, _ := root["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = map[string]interface{}{}
	}

	changed := false
	for _, he := range hookEvents {
		if mergeHookEvent(hooks, he.event, he.state, he.matcher, exe) {
			changed = true
		}
	}
	if !changed {
		return // 已是最新，不写、不备份
	}
	root["hooks"] = hooks

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return
	}
	// 先备份原文件，再写回（能走到这里说明原文件已存在且读取成功）
	os.WriteFile(settingsPath+".bak", orig, 0644)
	os.WriteFile(settingsPath, out, 0644)
}

// isGlightHook 判断一条 hook 条目是否属于任意版本的 Glight/claude-traffic-light。
// 双重判据：exe basename 符合命名规律 + args 是挂件约定的 ["hook", state]。
func isGlightHook(cmd string, args []interface{}) bool {
	base := strings.ToLower(filepath.Base(cmd))
	nameOK := base == "claude-traffic-light.exe" ||
		(strings.HasPrefix(base, "glight-v") && strings.HasSuffix(base, "-windows-amd64.exe"))
	if !nameOK {
		return false
	}
	if len(args) < 2 {
		return false
	}
	first, _ := args[0].(string)
	second, _ := args[1].(string)
	return first == "hook" && (second == "idle" || second == "thinking" || second == "running")
}

// mergeHookEvent 在 hooks[event] 里插入/更新挂件自己的 hook 组，返回是否有改动。
// 识别规则：isGlightHook 双重判据（exe 名 + args 约定）。
// - 非 Glight group → 原样保留
// - Glight group 且 basename == 当前 exe → 保留（路径变则更新）
// - Glight group 且 basename != 当前 exe（旧版本）→ 删除
// - 当前 exe 不存在 → 末尾追加
func mergeHookEvent(hooks map[string]interface{}, event, state string, matcher bool, exe string) bool {
	ourCmd := map[string]interface{}{
		"type":    "command",
		"command": exe,
		"args":    []interface{}{"hook", state},
	}
	ourGroup := map[string]interface{}{
		"hooks": []interface{}{ourCmd},
	}
	if matcher {
		ourGroup["matcher"] = "*"
	}

	arr, _ := hooks[event].([]interface{})
	newArr := make([]interface{}, 0, len(arr))
	foundCurrent := false
	changed := false

	for _, g := range arr {
		grp, ok := g.(map[string]interface{})
		if !ok {
			newArr = append(newArr, g)
			continue
		}
		inner, _ := grp["hooks"].([]interface{})

		// 判断这个 group 里的所有 hook 是否都属于 Glight
		allGlight := len(inner) > 0
		for _, h := range inner {
			hm, ok := h.(map[string]interface{})
			if !ok {
				allGlight = false
				break
			}
			hcmd, _ := hm["command"].(string)
			hargs, _ := hm["args"].([]interface{})
			if !isGlightHook(hcmd, hargs) {
				allGlight = false
				break
			}
		}
		if !allGlight {
			newArr = append(newArr, g) // 非 Glight group，不动
			continue
		}

		// 全是 Glight hook：看 basename 是否是当前 exe
		isCurrentExe := false
		for _, h := range inner {
			hm, _ := h.(map[string]interface{})
			hcmd, _ := hm["command"].(string)
			if strings.EqualFold(filepath.Base(hcmd), filepath.Base(exe)) {
				isCurrentExe = true
				break
			}
		}
		if !isCurrentExe {
			changed = true // 旧版本 group，删掉（不 append）
			continue
		}

		// 当前 exe 的 group
		if foundCurrent {
			changed = true // 重复条目，删掉多余的
			continue
		}
		foundCurrent = true
		// 检查路径是否已正确
		pathOK := false
		for _, h := range inner {
			hm, _ := h.(map[string]interface{})
			if hcmd, _ := hm["command"].(string); hcmd == exe {
				pathOK = true
				break
			}
		}
		if pathOK {
			newArr = append(newArr, g) // 路径未变，原样保留
		} else {
			newArr = append(newArr, ourGroup) // 路径变了（如换 U 盘盘符），更新
			changed = true
		}
	}

	if !foundCurrent {
		newArr = append(newArr, ourGroup)
		changed = true
	}
	if changed {
		hooks[event] = newArr
	}
	return changed
}
