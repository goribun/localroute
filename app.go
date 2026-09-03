package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"localroute/internal/config"
	"localroute/internal/requestlog"
	"localroute/internal/service"
)

type App struct {
	ctx        context.Context
	manager    *service.Manager
	configPath string
	logger     *slog.Logger
	requests   *requestlog.Store
	forwardPID int
}

func NewApp(configPath string, logger *slog.Logger) *App {
	requests := requestlog.New(1000)
	return &App{configPath: configPath, logger: logger, requests: requests, manager: service.New(configPath, logger, requests)}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if _, err := os.Stat(a.configPath); os.IsNotExist(err) {
		_ = config.Save(a.configPath, config.Default())
	}
	go a.manager.Watch(ctx)
	go func() {
		ch, cancel := a.requests.Subscribe()
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-ch:
				runtime.EventsEmit(ctx, "request:event", event)
			}
		}
	}()
}

func (a *App) shutdown(context.Context) {
	_ = a.Stop()
}

func (a *App) GetConfig() (config.Config, error) { return config.Load(a.configPath) }

func (a *App) SaveConfig(cfg config.Config) error {
	if err := config.Save(a.configPath, cfg); err != nil {
		return err
	}
	if a.manager.Status().Running {
		return a.manager.Reload()
	}
	return nil
}

func (a *App) Start() error {
	cfg, err := config.Load(a.configPath)
	if err != nil {
		return err
	}
	if !requiresPrivilegedPortForward(cfg.Listener.Port) {
		return a.manager.Start()
	}
	internal := config.Listener{Address: cfg.Listener.Address, Port: 18080}
	if err := a.manager.StartOn(&internal, cfg.Listener.String()); err != nil {
		return err
	}
	pid, err := startPrivilegedPortForward(cfg.Listener.String(), internal.String())
	if err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = a.manager.Stop(ctx)
		return err
	}
	a.forwardPID = pid
	return nil
}

func (a *App) Stop() error {
	if a.forwardPID != 0 {
		_ = stopPrivilegedPortForward(a.forwardPID)
		a.forwardPID = 0
	}
	_ = cleanupPrivilegedPortForward()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.manager.Stop(ctx)
}

func (a *App) Status() service.Status { return a.manager.Status() }

func (a *App) Validate(cfg config.Config) string {
	if err := cfg.Validate(); err != nil {
		return err.Error()
	}
	return ""
}

func (a *App) ConfigPath() string { return a.configPath }

func (a *App) Version() string { return version }
func (a *App) Requests() []requestlog.Event {
	requests := a.requests.List()
	if requests == nil {
		return []requestlog.Event{}
	}
	return requests
}
func (a *App) ClearRequests() {
	a.requests.Clear()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "request:cleared")
	}
}
func (a *App) Show() {
	if a.ctx != nil {
		runtime.WindowShow(a.ctx)
		runtime.WindowUnminimise(a.ctx)
	}
}
func (a *App) Hide() {
	if a.ctx != nil {
		runtime.WindowHide(a.ctx)
	}
}
func (a *App) Quit() {
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}

func (a *App) String() string { return fmt.Sprintf("LocalRoute %s", version) }

func (a *App) importConfig() (map[string]any, error) {
	if a.ctx == nil {
		return nil, errors.New("application is not ready")
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "导入 LocalRoute 配置",
		Filters: []runtime.FileFilter{{
			DisplayName: "YAML 配置 (*.yml, *.yaml)",
			Pattern:     "*.yml;*.yaml",
		}},
	})
	if err != nil {
		return nil, err
	}
	if path == "" {
		return map[string]any{"cancelled": true}, nil
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	return map[string]any{"cancelled": false, "config": cfg}, nil
}

func (a *App) exportConfig(cfg config.Config) (map[string]any, error) {
	if a.ctx == nil {
		return nil, errors.New("application is not ready")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出 LocalRoute 配置",
		DefaultFilename: config.FileName,
		Filters: []runtime.FileFilter{{
			DisplayName: "YAML 配置 (*.yml, *.yaml)",
			Pattern:     "*.yml;*.yaml",
		}},
	})
	if err != nil {
		return nil, err
	}
	if path == "" {
		return map[string]any{"cancelled": true}, nil
	}
	if err := config.Save(path, cfg); err != nil {
		return nil, err
	}
	return map[string]any{"cancelled": false, "path": path}, nil
}

func (a *App) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	write := func(value any) {
		if err := json.NewEncoder(w).Encode(value); err != nil {
			a.logger.Error("write GUI response", "error", err)
		}
	}
	fail := func(err error) { http.Error(w, err.Error(), http.StatusBadRequest) }
	switch r.Method + " " + r.URL.Path {
	case "GET /api/config":
		cfg, err := a.GetConfig()
		if err != nil {
			fail(err)
			return
		}
		write(cfg)
	case "PUT /api/config":
		var cfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			fail(err)
			return
		}
		if err := a.SaveConfig(cfg); err != nil {
			fail(err)
			return
		}
		write(map[string]bool{"ok": true})
	case "POST /api/config/import":
		result, err := a.importConfig()
		if err != nil {
			fail(err)
			return
		}
		write(result)
	case "POST /api/config/export":
		var cfg config.Config
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			fail(err)
			return
		}
		result, err := a.exportConfig(cfg)
		if err != nil {
			fail(err)
			return
		}
		write(result)
	case "GET /api/status":
		write(a.Status())
	case "GET /api/requests":
		write(a.Requests())
	case "GET /api/meta":
		write(map[string]string{"version": version, "configPath": a.configPath})
	case "POST /api/start":
		if err := a.Start(); err != nil {
			fail(err)
			return
		}
		write(map[string]bool{"ok": true})
	case "POST /api/stop":
		if err := a.Stop(); err != nil {
			fail(err)
			return
		}
		write(map[string]bool{"ok": true})
	case "POST /api/requests/clear":
		a.ClearRequests()
		write(map[string]bool{"ok": true})
	case "POST /api/quit":
		write(map[string]bool{"ok": true})
		go func() { time.Sleep(50 * time.Millisecond); a.Quit() }()
	case "POST /api/diagnostic":
		var diagnostic any
		if err := json.NewDecoder(r.Body).Decode(&diagnostic); err != nil {
			fail(err)
			return
		}
		a.logger.Info("GUI diagnostic", "state", diagnostic)
		write(map[string]bool{"ok": true})
	default:
		http.NotFound(w, r)
	}
}
