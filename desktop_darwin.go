//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c -fblocks
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
void localrouteDesktopStart(void);
void localrouteDesktopStop(void);
void localrouteDesktopActionFinished(void);
void localrouteDesktopUpdate(int running, const char *status);
*/
import "C"

import (
	"context"
	"sync"
	"time"
	"unsafe"
)

var desktopApp struct {
	sync.RWMutex
	app *App
}

func (a *App) startDesktop(ctx context.Context) {
	desktopApp.Lock()
	desktopApp.app = a
	desktopApp.Unlock()
	C.localrouteDesktopStart()
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			a.updateDesktop()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (a *App) stopDesktop() {
	desktopApp.Lock()
	desktopApp.app = nil
	desktopApp.Unlock()
	C.localrouteDesktopStop()
}

func (a *App) updateDesktop() {
	status := a.Status()
	label := "代理已停止"
	running := C.int(0)
	if status.Running {
		label = "代理运行中 · " + status.Listen
		running = 1
	}
	if status.LastError != "" {
		label = "代理异常 · " + status.LastError
	}
	value := C.CString(label)
	defer C.free(unsafe.Pointer(value))
	C.localrouteDesktopUpdate(running, value)
}

//export localrouteDesktopAction
func localrouteDesktopAction(action C.int) {
	desktopApp.RLock()
	a := desktopApp.app
	desktopApp.RUnlock()
	if a == nil {
		C.localrouteDesktopActionFinished()
		return
	}
	// Cocoa callbacks must never wait for Go operations which dispatch to Cocoa.
	go func() {
		switch action {
		case 0:
			a.Show()
		case 1:
			a.logger.Info("status bar proxy toggle requested")
			defer func() {
				C.localrouteDesktopActionFinished()
				a.updateDesktop()
			}()
			var err error
			if a.Status().Running {
				err = a.Stop()
			} else {
				err = a.Start()
			}
			if err != nil {
				a.logger.Error("status bar action failed", "error", err)
				a.Show()
			}
		case 2:
			a.Quit()
		case 3:
			a.recoverAfterWake()
			a.updateDesktop()
		}
	}()
}
