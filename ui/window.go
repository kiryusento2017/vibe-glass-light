package ui

import (
	"fmt"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"

	"claude-traffic-light/config"
	"claude-traffic-light/state"
)

const (
	winW = 120
	winH = 50
)

// Window manages the floating WebView2 window.
type Window struct {
	wv      webview2.WebView
	hwnd    windows.HWND
	cfg     config.Config
	cfgPath string
}

// New creates the floating WebView2 window.
func New(cfgPath string, cfg config.Config) *Window {
	w := &Window{cfg: cfg, cfgPath: cfgPath}

	wv := webview2.New(false)
	wv.SetTitle("Claude Traffic Light")
	wv.SetSize(winW, winH, webview2.HintFixed)
	wv.SetHtml(GlassHTML)
	w.wv = wv

	hwnd := windows.HWND(uintptr(wv.Window()))
	w.hwnd = hwnd
	applyWindowStyles(hwnd)

	// Position: default top-center, or saved position
	x := cfg.X
	if x < 0 {
		x = windowCenter(winW)
	}
	procSetWindowPos.Call(
		uintptr(hwnd), HWND_TOPMOST,
		uintptr(x), uintptr(cfg.Y),
		uintptr(winW), uintptr(winH),
		SWP_NOACTIVATE|SWP_FRAMECHANGED,
	)

	if cfg.ClickThrough {
		setPassthrough(hwnd, true)
	}

	// Bind: drag movement
	wv.Bind("__dragMove", func(dx, dy int) {
		var rect RECT
		procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rect)))
		newX := int(rect.Left) + dx
		newY := int(rect.Top) + dy
		procSetWindowPos.Call(
			uintptr(hwnd), 0,
			uintptr(newX), uintptr(newY), 0, 0,
			SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE,
		)
		w.cfg.X = newX
		w.cfg.Y = newY
	})

	// Bind: drag end — save position
	wv.Bind("__dragEnd", func() {
		config.Save(cfgPath, w.cfg)
	})

	// Bind: right-click context menu
	wv.Bind("__contextMenu", func() {
		wv.Dispatch(func() {
			w.showContextMenu()
		})
	})

	// Add system tray icon (display only — no message loop needed for MVP)
	w.addTrayIcon()

	return w
}

// SetState updates the traffic light via JS.
func (w *Window) SetState(s state.State) {
	w.wv.Dispatch(func() {
		w.wv.Eval(fmt.Sprintf("setState('%s')", s.String()))
	})
}

// Run starts the WebView2 message loop. Blocks until closed.
func (w *Window) Run() {
	defer w.removeTrayIcon()
	w.wv.Run()
}

func (w *Window) showContextMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	defer procDestroyMenu.Call(menu)

	visLabel := "隐藏窗口"
	if !w.cfg.Visible {
		visLabel = "显示窗口"
	}
	procAppendMenuW.Call(menu, MF_STRING, MENU_SHOW_HIDE,
		uintptr(unsafe.Pointer(u16(visLabel))))

	ptFlags := uintptr(MF_STRING)
	ptLabel := "开启穿透"
	if w.cfg.ClickThrough {
		ptFlags |= MF_CHECKED
		ptLabel = "关闭穿透"
	}
	procAppendMenuW.Call(menu, ptFlags, MENU_PASSTHROUGH,
		uintptr(unsafe.Pointer(u16(ptLabel))))

	procAppendMenuW.Call(menu, MF_SEPARATOR, 0, 0)
	procAppendMenuW.Call(menu, MF_STRING, MENU_EXIT,
		uintptr(unsafe.Pointer(u16("退出"))))

	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(uintptr(w.hwnd))

	cmd, _, _ := procTrackPopupMenu.Call(
		menu,
		TPM_RETURNCMD|TPM_RIGHTALIGN|TPM_BOTTOMALIGN,
		uintptr(pt.X), uintptr(pt.Y),
		0, uintptr(w.hwnd), 0,
	)

	switch cmd {
	case MENU_SHOW_HIDE:
		w.cfg.Visible = !w.cfg.Visible
		config.Save(w.cfgPath, w.cfg)
		// TODO: implement show/hide toggle
	case MENU_PASSTHROUGH:
		w.cfg.ClickThrough = !w.cfg.ClickThrough
		setPassthrough(w.hwnd, w.cfg.ClickThrough)
		config.Save(w.cfgPath, w.cfg)
	case MENU_EXIT:
		w.wv.Dispatch(func() {
			w.wv.Destroy()
		})
	}
}

func (w *Window) addTrayIcon() {
	tip := [128]uint16{}
	for i, r := range []rune("Claude Traffic Light") {
		if i >= 127 {
			break
		}
		tip[i] = uint16(r)
	}
	var hInst windows.Handle
	_ = windows.GetModuleHandleEx(0, nil, &hInst)
	hIcon, _, _ := procLoadIconW.Call(uintptr(hInst), IDI_APPLICATION)

	nid := NOTIFYICONDATAW{
		CbSize: uint32(unsafe.Sizeof(NOTIFYICONDATAW{})),
		HWnd:   w.hwnd,
		UID:    1,
		UFlags: NIF_ICON | NIF_TIP,
		HIcon:  windows.Handle(hIcon),
		SzTip:  tip,
	}
	procShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid)))
}

func (w *Window) removeTrayIcon() {
	nid := NOTIFYICONDATAW{
		CbSize: uint32(unsafe.Sizeof(NOTIFYICONDATAW{})),
		HWnd:   w.hwnd,
		UID:    1,
	}
	procShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))
}
