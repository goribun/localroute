//go:build windows

package main

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
	"unsafe"

	"localroute/internal/service"
)

func TestWindowsTrayABILayout(t *testing.T) {
	wantIcon, wantMessage, wantClass := uintptr(976), uintptr(48), uintptr(80)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		wantIcon, wantMessage, wantClass = 956, 32, 48
	}
	if unsafe.Sizeof(trayIconData{}) != wantIcon || unsafe.Sizeof(trayMessage{}) != wantMessage || unsafe.Sizeof(trayWindowClass{}) != wantClass {
		t.Fatalf("incorrect Windows ABI layouts: icon=%d message=%d class=%d", unsafe.Sizeof(trayIconData{}), unsafe.Sizeof(trayMessage{}), unsafe.Sizeof(trayWindowClass{}))
	}
}

func TestTrayTooltipTruncation(t *testing.T) {
	var text [128]uint16
	setTrayText(text[:], strings.Repeat("a", 126)+"😀suffix")
	if text[126] != 0 || text[127] != 0 {
		t.Fatal("truncation split a UTF-16 surrogate pair or omitted terminator")
	}
	setTrayText(text[:], "运行中")
	if string(utf16.Decode(text[:3])) != "运行中" || text[3] != 0 {
		t.Fatal("tooltip not replaced correctly")
	}
}

// Uses a real Win32 message queue, without starting a proxy or sleeping the host.
func TestWindowsTrayWakeAndShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wakes := make(chan struct{}, 4)
	tray := &windowsTray{
		status: func() service.Status { return service.Status{} },
		show:   func() {}, quit: func() {}, toggle: func() {},
		wake:   func() { wakes <- struct{}{} },
		report: func(err error) { t.Log(err) },
	}
	done := make(chan error, 1)
	go func() { done <- tray.run(ctx) }()
	deadline := time.After(5 * time.Second)
	for tray.snapshot.Load() == nil {
		select {
		case err := <-done:
			t.Fatalf("message loop exited early: %v", err)
		case <-deadline:
			t.Fatal("tray did not become ready")
		case <-time.After(10 * time.Millisecond):
		}
	}
	hwnd := tray.window.Load()
	trayPostMessage.Call(hwnd, wmPowerBroadcast, resumeAutomatic, 0)
	select {
	case <-wakes:
	case <-time.After(time.Second):
		t.Fatal("wake event was not delivered")
	}
	// RESUMESUSPEND is the second half of a user wake, not a second recovery.
	trayPostMessage.Call(hwnd, wmPowerBroadcast, 7, 0)
	select {
	case <-wakes:
		t.Fatal("duplicate recovery on RESUMESUSPEND")
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("tray message loop did not shut down")
	}
	if tray.window.Load() != 0 {
		t.Fatal("tray window was not released")
	}
}
