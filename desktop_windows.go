//go:build windows

package main

import (
	"context"
	"fmt"
	"runtime"
	"sync/atomic"
	"time"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
	"localroute/internal/service"
)

var (
	trayUser32        = windows.NewLazySystemDLL("user32.dll")
	trayShell32       = windows.NewLazySystemDLL("shell32.dll")
	trayRegisterClass = trayUser32.NewProc("RegisterClassExW")
	trayCreateWindow  = trayUser32.NewProc("CreateWindowExW")
	trayDefWindowProc = trayUser32.NewProc("DefWindowProcW")
	trayPostMessage   = trayUser32.NewProc("PostMessageW")
	trayNotifyIcon    = trayShell32.NewProc("Shell_NotifyIconW")
)

const (
	trayCallback     = 0x8001
	trayRefresh      = 0x8002
	trayFinished     = 0x8003
	wmClose          = 0x0010
	wmDestroy        = 0x0002
	wmPowerBroadcast = 0x0218
	resumeAutomatic  = 0x0012
	trayShow         = 1
	trayToggle       = 2
	trayQuit         = 3
)

// These layouts follow WNDCLASSEXW, MSG and NOTIFYICONDATAW (Windows SDK).
// uintptr fields preserve the native alignment on both 32- and 64-bit builds.
type trayWindowClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	SmallIcon                          uintptr
}
type trayPoint struct{ X, Y int32 }
type trayMessage struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          trayPoint
	Private        uint32
}
type trayIconData struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                windows.GUID
	BalloonIcon         uintptr
}

type windowsTray struct {
	status           func() service.Status
	show, quit, wake func()
	toggle           func()
	report           func(error)
	snapshot         atomic.Pointer[service.Status]
	window           atomic.Uintptr
	recovering       atomic.Bool
	// Everything below is owned by the message-loop thread.
	icon           trayIconData
	added, busy    bool
	taskbarCreated uint32
}

func (a *App) startDesktop(ctx context.Context) {
	tray := &windowsTray{
		status: a.Status, show: a.Show, quit: a.Quit, wake: a.recoverAfterWake,
		toggle: func() {
			var err error
			if a.Status().Running {
				err = a.Stop()
			} else {
				err = a.Start()
			}
			if err != nil {
				a.logger.Error("tray proxy toggle failed", "error", err)
				a.Show()
			}
		},
		report: func(err error) { a.logger.Error("Windows tray", "error", err) },
	}
	done := make(chan struct{})
	a.desktopDone = done
	go func() {
		defer close(done)
		if err := tray.run(ctx); err != nil {
			tray.report(err)
		}
	}()
}

// Cancelling desktopCtx in shutdown asks the tray's own thread to destroy it.
func (a *App) stopDesktop() {
	if a.desktopDone != nil {
		select {
		case <-a.desktopDone:
		case <-time.After(2 * time.Second):
			a.logger.Warn("timed out waiting for Windows tray shutdown")
		}
	}
}

func (t *windowsTray) post(message uint32) {
	if hwnd := t.window.Load(); hwnd != 0 {
		trayPostMessage.Call(hwnd, uintptr(message), 0, 0)
	}
}

func (t *windowsTray) run(ctx context.Context) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	className, _ := windows.UTF16PtrFromString("LocalRoute.TrayWindow")
	var instance windows.Handle
	err := windows.GetModuleHandleEx(windows.GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT, nil, &instance)
	if err != nil {
		return err
	}
	callback := windows.NewCallback(t.windowProc)
	wc := trayWindowClass{Proc: callback, Instance: uintptr(instance), ClassName: className}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if result, _, err := trayRegisterClass.Call(uintptr(unsafe.Pointer(&wc))); result == 0 {
		return fmt.Errorf("register tray window: %w", err)
	}
	defer trayUser32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(className)), uintptr(instance))
	// A hidden top-level window (not HWND_MESSAGE) receives power and Explorer broadcasts.
	hwnd, _, err := trayCreateWindow.Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(className)), 0, 0, 0, 0, 0, 0, 0, uintptr(instance), 0)
	if hwnd == 0 {
		return fmt.Errorf("create tray window: %w", err)
	}
	t.window.Store(hwnd)
	defer t.window.Store(0)
	taskbarName, _ := windows.UTF16PtrFromString("TaskbarCreated")
	taskbar, _, _ := trayUser32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(taskbarName)))
	t.taskbarCreated = uint32(taskbar)
	icon, _, _ := trayUser32.NewProc("LoadIconW").Call(uintptr(instance), 3) // Wails app icon resource.
	if icon == 0 {
		icon, _, _ = trayUser32.NewProc("LoadIconW").Call(0, 32512)
	}
	t.icon = trayIconData{Window: hwnd, ID: 1, Flags: 1 | 2 | 4 | 0x80, Callback: trayCallback, Icon: icon}
	t.icon.Size = uint32(unsafe.Sizeof(t.icon))
	t.refreshIcon()
	defer func() { trayNotifyIcon.Call(2, uintptr(unsafe.Pointer(&t.icon))) }()
	// Opt in to notifications on systems with Modern Standby as well.
	registerPower := trayUser32.NewProc("RegisterSuspendResumeNotification")
	if registerPower.Find() == nil {
		registration, _, err := registerPower.Call(hwnd, 0)
		if registration != 0 {
			defer trayUser32.NewProc("UnregisterSuspendResumeNotification").Call(registration)
		} else {
			t.report(fmt.Errorf("register wake notifications: %w", err))
		}
	}
	loopCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				t.post(wmClose)
				return
			case <-ticker.C:
				status := t.status()
				t.snapshot.Store(&status)
				t.post(trayRefresh)
			}
		}
	}()
	var msg trayMessage
	for {
		result, _, err := trayUser32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) == -1 {
			trayUser32.NewProc("DestroyWindow").Call(hwnd)
			return fmt.Errorf("tray message loop: %w", err)
		}
		if result == 0 {
			return nil
		}
		trayUser32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&msg)))
		trayUser32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func (t *windowsTray) windowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	if t.taskbarCreated != 0 && message == t.taskbarCreated {
		t.added = false
		t.refreshIcon()
		return 0
	}
	switch message {
	case trayCallback:
		switch uint32(lParam) & 0xffff {
		case 0x0400, 0x0401:
			go t.show() // left button up
		case 0x0205, 0x007b:
			t.showMenu(hwnd) // right button / keyboard context menu
		}
		return 0
	case trayRefresh:
		t.refreshIcon()
		return 0
	case trayFinished:
		t.busy = false
		t.refreshIcon()
		return 0
	case wmPowerBroadcast:
		// Windows sends RESUMESUSPEND after RESUMEAUTOMATIC for user-initiated
		// wakes. Handle only the latter to avoid resetting fresh connections twice.
		if wParam == resumeAutomatic && t.recovering.CompareAndSwap(false, true) {
			go func() { defer t.recovering.Store(false); t.wake() }()
		}
		return 1
	case wmClose:
		trayUser32.NewProc("DestroyWindow").Call(hwnd)
		return 0
	case wmDestroy:
		t.window.Store(0)
		trayUser32.NewProc("PostQuitMessage").Call(0)
		return 0
	}
	result, _, _ := trayDefWindowProc.Call(hwnd, uintptr(message), wParam, lParam)
	return result
}

func (t *windowsTray) currentStatus() service.Status {
	if status := t.snapshot.Load(); status != nil {
		return *status
	}
	return service.Status{}
}

func (t *windowsTray) refreshIcon() {
	label, _ := windowsTrayLabels(t.currentStatus(), t.busy)
	setTrayText(t.icon.Tip[:], "LocalRoute · "+label)
	operation := uintptr(1) // NIM_MODIFY
	if !t.added {
		operation = 0
	} // NIM_ADD, also retries if Explorer wasn't ready.
	result, _, _ := trayNotifyIcon.Call(operation, uintptr(unsafe.Pointer(&t.icon)))
	t.added = result != 0
	if t.added && operation == 0 {
		t.icon.Version = 4
		trayNotifyIcon.Call(4, uintptr(unsafe.Pointer(&t.icon))) // NIM_SETVERSION
	}
}

func (t *windowsTray) showMenu(hwnd uintptr) {
	menu, _, _ := trayUser32.NewProc("CreatePopupMenu").Call()
	if menu == 0 {
		return
	}
	defer trayUser32.NewProc("DestroyMenu").Call(menu)
	appendItem := func(flags, id uintptr, text string) {
		value, _ := windows.UTF16PtrFromString(text)
		trayUser32.NewProc("AppendMenuW").Call(menu, flags, id, uintptr(unsafe.Pointer(value)))
	}
	label, toggle := windowsTrayLabels(t.currentStatus(), t.busy)
	appendItem(2, 0, label) // MF_DISABLED
	appendItem(0x800, 0, "")
	appendItem(0, trayShow, "打开 LocalRoute")
	flags := uintptr(0)
	if t.busy {
		flags = 2
	}
	appendItem(flags, trayToggle, toggle)
	appendItem(0x800, 0, "")
	appendItem(0, trayQuit, "退出 LocalRoute")
	var point trayPoint
	trayUser32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&point)))
	trayUser32.NewProc("SetForegroundWindow").Call(hwnd)
	selected, _, _ := trayUser32.NewProc("TrackPopupMenu").Call(menu, 0x100|0x2, uintptr(point.X), uintptr(point.Y), 0, hwnd, 0)
	trayPostMessage.Call(hwnd, 0, 0, 0) // WM_NULL: allow dismissal of subsequent menus.
	switch selected {
	case trayShow:
		go t.show()
	case trayQuit:
		go t.quit()
	case trayToggle:
		if t.busy {
			return
		}
		t.busy = true
		t.refreshIcon()
		go func() {
			t.toggle()
			status := t.status()
			t.snapshot.Store(&status)
			t.post(trayFinished)
		}()
	}
}

func setTrayText(dst []uint16, text string) {
	if len(dst) == 0 {
		return
	}
	clear(dst)
	encoded := utf16.Encode([]rune(text))
	n := min(len(encoded), len(dst)-1)
	if n > 0 && encoded[n-1] >= 0xd800 && encoded[n-1] <= 0xdbff {
		n--
	}
	copy(dst, encoded[:n])
}
