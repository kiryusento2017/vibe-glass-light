package ui

import (
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	d3d11 "github.com/kirides/go-d3d/d3d11"
	"golang.org/x/sys/windows"

	"claude-traffic-light/config"
	"claude-traffic-light/state"
)

// winW/winH 是窗口画布尺寸；玻璃 pill 逻辑尺寸（250×88）在 glass.hlsl 内，
// 居中在画布里，多出的 margin 容纳形变（稳态拉伸 + 松手过冲）。两者须与
// glass.hlsl 的 CANVAS 常量保持一致。
const (
	winW = 270
	winH = 160
)

// theWindow 是当前唯一的挂件窗口（单实例由 main.go 的互斥保证）。
// wndProc 是包级回调，通过它访问实例。
var theWindow *Window

// Window 管理浮动挂件窗口：主线程跑消息循环，渲染在独立 goroutine，
// 拖动的系统模态循环挡不住渲染。
type Window struct {
	hwnd       windows.HWND
	hInst      windows.Handle
	cfg        config.Config
	cfgPath    string
	tuningPath string // glass-tuning.json（exe 同目录，渲染线程热重载）
	curState   atomic.Int32 // state.State
	closing    atomic.Bool

	// 自接管鼠标拖拽：取消系统 caption 拖动，自己监听按下/移动/松开，
	// 为弹簧形变（第 4 步）提供按下/松开/速度事件。
	dragStart  POINT // 拖拽起始光标坐标（屏幕像素）
	winStart   POINT // 拖拽起始窗口位置（屏幕像素）
	lastCursor POINT // 上一帧光标位置（算拖动速度用）
	speedX     float32
	speedY     float32
	pressed    atomic.Bool

	// 弹簧形变状态（主线程鼠标事件写，渲染线程每帧积分读；sync.Mutex 保护）
	deformMu sync.Mutex
	deform   [2]Spring // [0]=X轴(水平缩放) [1]=Y轴(垂直缩放)
	// 弹簧参数（渲染线程热重载 tuning 时同步更新，鼠标事件只读）
	pressX, pressY, steadyX, steadyY, dragK, dragMin, releaseImpulse float32
}

// New 创建挂件窗口并启动渲染线程。必须在将要跑消息循环的线程上调用。
func New(cfgPath string, cfg config.Config) *Window {
	runtime.LockOSThread()
	setThreadDPIAware()

	w := &Window{cfg: cfg, cfgPath: cfgPath}
	// glass-tuning.json 与 config.json 同目录；首次运行生成默认文件供用户编辑。
	w.tuningPath = filepath.Join(filepath.Dir(cfgPath), "glass-tuning.json")
	if _, err := os.Stat(w.tuningPath); os.IsNotExist(err) {
		config.SaveTuning(w.tuningPath, config.DefaultTuning())
	}
	w.curState.Store(int32(state.Grey))
	// 形变弹簧初始化为稳态（静止时的上下拉伸/左右变窄）
	dt := config.DefaultTuning()
	w.pressX, w.pressY = dt.PressX, dt.PressY
	w.steadyX, w.steadyY = dt.SteadyX, dt.SteadyY
	w.dragK = dt.DragK
	w.dragMin = dt.DragMin
	w.releaseImpulse = dt.ReleaseImpulse
	w.deform[0] = Spring{K: dt.SpringK, C: dt.SpringC, Target: dt.SteadyX, Pos: dt.SteadyX}
	w.deform[1] = Spring{K: dt.SpringK, C: dt.SpringC, Target: dt.SteadyY, Pos: dt.SteadyY}
	theWindow = w

	var hInst windows.Handle
	windows.GetModuleHandleEx(0, nil, &hInst)
	w.hInst = hInst

	className := u16("ClaudeTrafficLightWnd")
	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		style:         csHRedraw | csVRedraw,
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     hInst,
		lpszClassName: className,
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	// 位置：水平居中、贴屏幕顶部（Y=16），或使用保存的位置
	x, _ := screenCenter(winW, winH)
	y := 16
	if cfg.X >= 0 {
		x, y = cfg.X, cfg.Y
	}
	w.cfg.X, w.cfg.Y = x, y

	hwnd, _, _ := procCreateWindowExW.Call(
		wsExNoredirectionbitmap|wsExTopmost|wsExToolwindow|wsExNoactivate,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(u16("Claude Traffic Light"))),
		wsPopup,
		uintptr(x), uintptr(y), winW, winH,
		0, 0, uintptr(hInst), 0,
	)
	w.hwnd = windows.HWND(hwnd)

	// 命门：把窗口从屏幕捕获中排除，断开「自己折射自己」反馈
	procSetWindowDisplayAffinity.Call(hwnd, wdaExcludeFromCapture)

	go w.renderThread()

	if cfg.Visible {
		// 一次 SetWindowPos 同时「升到 topmost + 显示」，避免先显示再置顶的中间帧。
		// （WS_EX_TOPMOST 对 NOACTIVATE 窗口创建时不一定真正升到 topmost z-order。）
		procSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0,
			swpNoMove|swpNoSize|swpNoActivate|swpShowWindow)
	}
	// 周期性重新提顶：DComp 内容由渲染线程异步呈现，单次提顶可能被时序打破；
	// 定时器在主线程（含拖动模态循环）周期恢复 topmost。
	procSetTimer.Call(hwnd, 1, 300, 0)

	w.addTrayIcon()

	return w
}

// SetState 线程安全地更新红绿灯状态（渲染线程读取）。
func (w *Window) SetState(s state.State) {
	w.curState.Store(int32(s))
}

// Run 跑主线程消息循环，阻塞至窗口销毁。
func (w *Window) Run() {
	defer w.removeTrayIcon()
	var m MSG
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	w.closing.Store(true)
}

// wndProc 处理窗口消息。自接管鼠标拖拽（取消系统 caption 拖动），获取按下/移动/松开
// 事件以驱动弹簧形变；Locked 模式保留按压反馈但不挪窗。右键菜单改为客户区右键。
func wndProc(hwnd, message, wParam, lParam uintptr) uintptr {
	switch message {
	case wmNcHitTest:
		return htClient // 恒为客户区：取消系统拖动，由自接管鼠标处理

	case wmSetCursor:
		// DComp/NOREDIRECTIONBITMAP 窗上系统默认等待光标，强制箭头。
		arrow, _, _ := procLoadCursorW.Call(0, idcArrow)
		procSetCursor.Call(arrow)
		return 1

	case wmLButtonDown:
		theWindow.pressed.Store(true)
		var pt POINT
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		theWindow.dragStart = pt
		theWindow.lastCursor = pt
		theWindow.speedX = 0
		theWindow.speedY = 0
		var wr RECT
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
		theWindow.winStart = POINT{X: wr.Left, Y: wr.Top}
		procSetCapture.Call(hwnd)
		// 按软：横向胀、纵向扁
		theWindow.deformMu.Lock()
		theWindow.deform[0].Target = theWindow.pressX
		theWindow.deform[1].Target = theWindow.pressY
		theWindow.deformMu.Unlock()
		return 0

	case wmMouseMove:
		if !theWindow.pressed.Load() {
			break
		}
		var pt POINT
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		dx := pt.X - theWindow.dragStart.X
		dy := pt.Y - theWindow.dragStart.Y
		// 拖动速度（像素/帧）→ 拖得越快越窄
		theWindow.speedX = float32(pt.X - theWindow.lastCursor.X)
		theWindow.speedY = float32(pt.Y - theWindow.lastCursor.Y)
		theWindow.lastCursor = pt
		sv := theWindow
		speedMag := float32(math.Sqrt(float64(theWindow.speedX*theWindow.speedX + theWindow.speedY*theWindow.speedY)))
		dragScale := 1.0 - speedMag*sv.dragK
		if dragScale < sv.dragMin {
			dragScale = sv.dragMin
		}
		theWindow.deformMu.Lock()
		theWindow.deform[0].Target = sv.pressX * dragScale // 横向压扁后因拖动更窄
		theWindow.deform[1].Target = sv.pressY / dragScale // 竖向补偿
		theWindow.deformMu.Unlock()
		if theWindow.cfg.Locked {
			break // 锁定：不挪窗，但仍记下按压（视觉反馈保留）
		}
		nx := int(theWindow.winStart.X) + int(dx)
		ny := int(theWindow.winStart.Y) + int(dy)
		procSetWindowPos.Call(hwnd, 0, uintptr(nx), uintptr(ny), 0, 0,
			swpNoSize|swpNoZOrder|swpNoActivate)
		return 0

	case wmLButtonUp:
		theWindow.pressed.Store(false)
		procReleaseCapture.Call()
		// 松手：回到稳态 + 基于松开前速度给过冲冲量
		theWindow.deformMu.Lock()
		theWindow.deform[0].Target = theWindow.steadyX
		theWindow.deform[0].Vel += theWindow.speedX * theWindow.releaseImpulse * 0.0005
		theWindow.deform[1].Target = theWindow.steadyY
		theWindow.deform[1].Vel += theWindow.speedY * theWindow.releaseImpulse * 0.0005
		theWindow.deformMu.Unlock()
		// 保存新位置
		var wr RECT
		procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
		theWindow.cfg.X, theWindow.cfg.Y = int(wr.Left), int(wr.Top)
		config.Save(theWindow.cfgPath, theWindow.cfg)
		return 0

	case wmRButtonUp:
		theWindow.showContextMenu()
		return 0

	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0

	case wmTimer:
		procSetWindowPos.Call(hwnd, hwndTopmost, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
		return 0

	case wmTray:
		if lParam == wmRButtonUp || lParam == wmLButtonUp {
			theWindow.showContextMenu()
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, message, wParam, lParam)
	return r
}

// renderThread 拥有 D3D device 与渲染循环，独立于 UI 线程。
// Task 3a：先画半透明纯色，验证透明置顶窗与拖动实时性。
func (w *Window) renderThread() {
	runtime.LockOSThread()
	setThreadDPIAware()

	device, ctx, err := d3d11.NewD3D11Device()
	if err != nil {
		return // 降级：无 GPU/被限制时不渲染（Task 9 完善重试）
	}
	defer device.Release()
	defer ctx.Release()
	dev := uintptr(unsafe.Pointer(device))
	dctx := uintptr(unsafe.Pointer(ctx))

	dxgiDevice, factory, err := queryDXGIFactory(dev)
	if err != nil {
		return
	}
	swapchain, err := createCompositionSwapchain(factory, dev, winW, winH)
	if err != nil {
		return
	}
	rtv, err := backBufferRTV(dev, swapchain)
	if err != nil {
		return
	}
	if _, err := dcompAttach(dxgiDevice, uintptr(w.hwnd), swapchain); err != nil {
		return
	}

	// 建抓屏与折射渲染器；任一失败则降级为不渲染（Task 9 完善重试）
	capt, err := newCapture(dev, dctx)
	if err != nil {
		return
	}
	defer capt.Release()
	renderer, err := newRenderer(dev, dctx)
	if err != nil {
		return
	}
	defer renderer.Release()

	start := time.Now()
	last := start
	first := true
	tun, _ := config.LoadTuning(w.tuningPath) // 视觉参数初值（不存在→默认）
	reloadN := 0
	for !w.closing.Load() {
		// 每 ~60 帧（约 0.5s）热重载 glass-tuning.json：保存即生效
		reloadN++
		if reloadN >= 60 {
			reloadN = 0
			if nt, err := config.LoadTuning(w.tuningPath); err == nil {
				tun = nt
				// 同步弹簧参数到主线程可读字段
				w.deformMu.Lock()
				w.pressX, w.pressY = nt.PressX, nt.PressY
				w.steadyX, w.steadyY = nt.SteadyX, nt.SteadyY
				w.dragK = nt.DragK
				w.dragMin = nt.DragMin
				w.releaseImpulse = nt.ReleaseImpulse
				w.deform[0].K = nt.SpringK
				w.deform[0].C = nt.SpringC
				w.deform[1].K = nt.SpringK
				w.deform[1].C = nt.SpringC
				w.deformMu.Unlock()
			}
		}
		// 形变弹簧每帧积分
		w.deformMu.Lock()
		now := time.Now()
		dt := float32(now.Sub(last).Seconds())
		if dt > 0.05 {
			dt = 0.05 // 钳制：单帧不跳过 50ms（防止卡顿拉飞）
		}
		last = now
		w.deform[0].Integrate(dt)
		w.deform[1].Integrate(dt)
		sx, sy := w.deform[0].Pos, w.deform[1].Pos
		w.deformMu.Unlock()

		var wr RECT
		procGetWindowRect.Call(uintptr(w.hwnd), uintptr(unsafe.Pointer(&wr)))
		srv, _ := capt.AcquireTexture(wr) // 桌面静止时复用上一帧 SRV
		if srv != 0 {
			active := float32(w.curState.Load())
			t := time.Since(start).Seconds()
			blink := float32(0.5 + 0.5*math.Sin(2*math.Pi*t/0.85))
			renderer.Frame(rtv, srv, active, blink, sx, sy, tun)
			comCall(swapchain, vtSwapPresent, 0, 0)
		}
		if first {
			// 内容首帧呈现的此刻确保 topmost（DComp 异步呈现晚于 New 的提顶时机）
			procSetWindowPos.Call(uintptr(w.hwnd), hwndTopmost, 0, 0, 0, 0,
				swpNoMove|swpNoSize|swpNoActivate)
			first = false
		}
		time.Sleep(8 * time.Millisecond)
	}
}

// addTrayIcon 注册系统托盘图标，鼠标事件回调为 wmTray 消息。
func (w *Window) addTrayIcon() {
	var tip [128]uint16
	for i, c := range windows.StringToUTF16("Claude Traffic Light") {
		if i >= len(tip) {
			break
		}
		tip[i] = c
	}
	hIcon, _, _ := procLoadIconW.Call(0, idiApplication)
	nid := NOTIFYICONDATAW{
		CbSize:           uint32(unsafe.Sizeof(NOTIFYICONDATAW{})),
		HWnd:             w.hwnd,
		UID:              1,
		UFlags:           nifIcon | nifTip | nifMessage,
		UCallbackMessage: wmTray,
		HIcon:            windows.Handle(hIcon),
		SzTip:            tip,
	}
	procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid)))
}

// removeTrayIcon 移除托盘图标。
func (w *Window) removeTrayIcon() {
	nid := NOTIFYICONDATAW{
		CbSize: uint32(unsafe.Sizeof(NOTIFYICONDATAW{})),
		HWnd:   w.hwnd,
		UID:    1,
	}
	procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
}

// showContextMenu 弹出右键菜单：显示/隐藏窗口、固定/不固定位置、退出。
func (w *Window) showContextMenu() {
	menu, _, _ := procCreatePopupMenu.Call()
	defer procDestroyMenu.Call(menu)

	visLabel := "隐藏窗口"
	if !w.cfg.Visible {
		visLabel = "显示窗口"
	}
	procAppendMenuW.Call(menu, mfString, menuShowHide, uintptr(unsafe.Pointer(u16(visLabel))))

	lockFlags := uintptr(mfString)
	lockLabel := "固定位置"
	if w.cfg.Locked {
		lockFlags |= mfChecked
		lockLabel = "不固定位置"
	}
	procAppendMenuW.Call(menu, lockFlags, menuLock, uintptr(unsafe.Pointer(u16(lockLabel))))

	procAppendMenuW.Call(menu, mfSeparator, 0, 0)
	procAppendMenuW.Call(menu, mfString, menuExit, uintptr(unsafe.Pointer(u16("退出"))))

	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(uintptr(w.hwnd))

	cmd, _, _ := procTrackPopupMenu.Call(menu,
		tpmReturnCmd|tpmRightAlign|tpmBottomAlign,
		uintptr(pt.X), uintptr(pt.Y), 0, uintptr(w.hwnd), 0)

	switch cmd {
	case menuShowHide:
		w.cfg.Visible = !w.cfg.Visible
		if w.cfg.Visible {
			procSetWindowPos.Call(uintptr(w.hwnd), hwndTopmost, 0, 0, 0, 0,
				swpNoMove|swpNoSize|swpNoActivate|swpShowWindow)
		} else {
			procShowWindow.Call(uintptr(w.hwnd), swHide)
		}
		config.Save(w.cfgPath, w.cfg)
	case menuLock:
		w.cfg.Locked = !w.cfg.Locked
		config.Save(w.cfgPath, w.cfg)
	case menuExit:
		procDestroyWindow.Call(uintptr(w.hwnd))
	}
}
