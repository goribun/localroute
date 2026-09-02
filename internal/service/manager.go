package service

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"localroute/internal/config"
	proxyserver "localroute/internal/proxy"
	"localroute/internal/requestlog"
)

type Status struct {
	Running    bool      `json:"running"`
	Listen     string    `json:"listen"`
	ConfigPath string    `json:"configPath"`
	StartedAt  time.Time `json:"startedAt,omitempty"`
	LastError  string    `json:"lastError,omitempty"`
}

type Manager struct {
	mu       sync.RWMutex
	server   *proxyserver.Server
	status   Status
	logger   *slog.Logger
	requests *requestlog.Store
}

func New(configPath string, logger *slog.Logger, stores ...*requestlog.Store) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	store := requestlog.New(1000)
	if len(stores) > 0 && stores[0] != nil {
		store = stores[0]
	}
	return &Manager{logger: logger, requests: store, status: Status{ConfigPath: configPath}}
}

func (m *Manager) Start() error {
	return m.StartOn(nil, "")
}

func (m *Manager) StartOn(listener *config.Listener, advertised string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status.Running {
		return nil
	}
	cfg, err := config.Load(m.status.ConfigPath)
	if err != nil {
		m.status.LastError = err.Error()
		return err
	}
	if listener != nil {
		cfg.Listener = *listener
	}
	server, err := proxyserver.New(cfg, m.logger, m.requests)
	if err != nil {
		m.status.LastError = err.Error()
		return err
	}
	m.server = server
	if advertised == "" {
		advertised = cfg.Listener.String()
	}
	m.status.Running, m.status.Listen, m.status.StartedAt, m.status.LastError = true, advertised, time.Now(), ""
	go func() {
		if err := server.ListenAndServe(); err != nil {
			m.mu.Lock()
			if m.server == server {
				m.status.Running = false
				m.status.LastError = err.Error()
			}
			m.mu.Unlock()
			m.logger.Error("proxy stopped", "error", err)
		}
	}()
	return nil
}

func (m *Manager) Stop(ctx context.Context) error {
	m.mu.RLock()
	server := m.server
	m.mu.RUnlock()
	if server == nil {
		return nil
	}
	err := server.Shutdown(ctx)
	m.mu.Lock()
	m.server = nil
	m.status.Running = false
	if err != nil {
		m.status.LastError = err.Error()
	}
	m.mu.Unlock()
	return err
}

func (m *Manager) Reload() error {
	cfg, err := config.Load(m.status.ConfigPath)
	if err != nil {
		m.setError(err)
		return err
	}
	m.mu.RLock()
	server := m.server
	currentListen := m.status.Listen
	m.mu.RUnlock()
	if server == nil {
		return nil
	}
	if cfg.Listener.String() != currentListen && currentListen == server.ListenAddress() {
		return errors.New("listen address changed; stop and start LocalRoute to apply it")
	}
	if err := server.Reload(cfg); err != nil {
		m.setError(err)
		return err
	}
	m.mu.Lock()
	m.status.LastError = ""
	m.mu.Unlock()
	return nil
}

func (m *Manager) Status() Status { m.mu.RLock(); defer m.mu.RUnlock(); return m.status }

func (m *Manager) setError(err error) { m.mu.Lock(); m.status.LastError = err.Error(); m.mu.Unlock() }

func (m *Manager) Watch(ctx context.Context) {
	var lastMod time.Time
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			info, err := os.Stat(m.status.ConfigPath)
			if err != nil {
				continue
			}
			if lastMod.IsZero() {
				lastMod = info.ModTime()
				continue
			}
			if info.ModTime().After(lastMod) {
				lastMod = info.ModTime()
				if err := m.Reload(); err != nil {
					m.logger.Error("configuration reload failed; keeping previous routes", "error", err)
				}
			}
		}
	}
}
