package watcher

import (
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32P                   = windows.NewLazySystemDLL("kernel32.dll")
	procCreateToolhelp32Snapshot = kernel32P.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW          = kernel32P.NewProc("Process32FirstW")
	procProcess32NextW           = kernel32P.NewProc("Process32NextW")
	procCloseHandle              = kernel32P.NewProc("CloseHandle")
)

const th32csSnapprocess = 2

type processEntry32W struct {
	dwSize              uint32
	cntUsage            uint32
	th32ProcessID       uint32
	th32DefaultHeapID   uintptr
	th32ModuleID        uint32
	cntThreads          uint32
	th32ParentProcessID uint32
	pcPriClassBase      int32
	dwFlags             uint32
	szExeFile           [260]uint16
}

// isClaudeCodeRunning 枚举所有进程，检查是否有 Claude Code 进程。
// 匹配规则：进程名 claude.exe（不区分大小写）。
// Claude Code 可能在任意宿主中运行（CLI / VS Code / Cursor 扩展），
// 但无论哪种方式，都会 spawn 一个名为 claude.exe 的进程。
func isClaudeCodeRunning() bool {
	h, _, _ := procCreateToolhelp32Snapshot.Call(th32csSnapprocess, 0)
	if h == 0 || h == ^uintptr(0) {
		return false
	}
	defer syscall.CloseHandle(syscall.Handle(h))

	var pe processEntry32W
	pe.dwSize = uint32(unsafe.Sizeof(pe))
	r, _, _ := procProcess32FirstW.Call(h, uintptr(unsafe.Pointer(&pe)))
	for r != 0 {
		name := strings.ToLower(windows.UTF16ToString(pe.szExeFile[:]))
		if name == "claude.exe" {
			return true
		}
		pe.dwSize = uint32(unsafe.Sizeof(pe))
		r, _, _ = procProcess32NextW.Call(h, uintptr(unsafe.Pointer(&pe)))
	}
	return false
}
