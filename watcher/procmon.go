package watcher

import (
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32P                    = windows.NewLazySystemDLL("kernel32.dll")
	procCreateToolhelp32Snapshot = kernel32P.NewProc("CreateToolhelp32Snapshot")
	procProcess32FirstW          = kernel32P.NewProc("Process32FirstW")
	procProcess32NextW           = kernel32P.NewProc("Process32NextW")
	procCloseHandle              = kernel32P.NewProc("CloseHandle")
	procOpenProcess              = kernel32P.NewProc("OpenProcess")
	procGetPackageFamilyName     = kernel32P.NewProc("GetPackageFamilyName")
)

const (
	th32csSnapprocess       = 2
	processQueryLimitedInfo = 0x1000
)

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
// 匹配规则：进程名 claude.exe（不区分大小写）且「不是 MSIX 打包应用」。
// Claude Code 可能在任意宿主中运行（CLI 原生 / npm 全局 / VS Code / Cursor 扩展），
// 各宿主安装路径不同，无统一路径特征，但都是普通 exe（无 package identity）。
// Claude Desktop 商店版是 MSIX 打包应用，进程有 package identity，靠此排除。
// 失败方向安全：直装版 Desktop（非 MSIX）或 OpenProcess 失败时退化为「不排除」
// （Desktop 开着时不降灰的小瑕疵），绝不会误杀真实 Claude Code。详见 CLAUDE.md 踩坑记录。
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
		if name == "claude.exe" && !isMsixPackaged(pe.th32ProcessID) {
			return true
		}
		pe.dwSize = uint32(unsafe.Sizeof(pe))
		r, _, _ = procProcess32NextW.Call(h, uintptr(unsafe.Pointer(&pe)))
	}
	return false
}

// isMsixPackaged 判断进程是否为 MSIX 打包应用（有 package identity）。
// GetPackageFamilyName 对打包应用会把所需缓冲长度写入 length（>0）；无包应用返回
// APPMODEL_ERROR_NO_PACKAGE，length 保持 0。只看 length 是否 >0，无需取出包名。
// 打不开进程句柄时保守返回 false（当作非 MSIX，失败方向安全：不排除）。
func isMsixPackaged(pid uint32) bool {
	hp, _, _ := procOpenProcess.Call(processQueryLimitedInfo, 0, uintptr(pid))
	if hp == 0 {
		return false
	}
	defer syscall.CloseHandle(syscall.Handle(hp))

	var length uint32
	procGetPackageFamilyName.Call(hp, uintptr(unsafe.Pointer(&length)), 0)
	return length > 0
}
