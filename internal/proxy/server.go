package proxy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"localroute/internal/config"
	"localroute/internal/requestlog"
	"localroute/internal/router"
)

type Server struct {
	mu         sync.RWMutex
	httpServer *http.Server
	table      *router.Table
	logger     *slog.Logger
	listen     string
	requests   *requestlog.Store
}

func New(cfg config.Config, logger *slog.Logger, stores ...*requestlog.Store) (*Server, error) {
	table, err := router.New(cfg)
	if err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	store := requestlog.New(1000)
	if len(stores) > 0 && stores[0] != nil {
		store = stores[0]
	}
	listen := cfg.Listener.String()
	s := &Server{table: table, logger: logger, listen: listen, requests: store}
	s.httpServer = &http.Server{Addr: listen, Handler: s, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second}
	return s, nil
}

func (s *Server) ListenAndServe() error {
	s.logger.Info("proxy started", "listen", s.listen)
	err := s.httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) Shutdown(ctx context.Context) error { return s.httpServer.Shutdown(ctx) }
func (s *Server) ListenAddress() string              { return s.listen }

func (s *Server) Reload(cfg config.Config) error {
	table, err := router.New(cfg)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.table = table
	s.mu.Unlock()
	s.logger.Info("configuration reloaded")
	return nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	s.mu.RLock()
	route, ok := s.table.Match(r.Host, r.Method, r.URL.Path)
	s.mu.RUnlock()
	if !ok {
		http.Error(w, "LocalRoute: no route for host "+r.Host, http.StatusNotFound)
		s.requests.Add(requestlog.Event{Time: started, Method: r.Method, Host: r.Host, Path: r.URL.Path, Status: http.StatusNotFound, DurationMS: time.Since(started).Milliseconds(), Error: "route not found"})
		s.logger.Warn("route not found", "method", r.Method, "host", r.Host, "path", r.URL.Path)
		return
	}
	target, err := url.Parse(route.Target)
	if err != nil {
		http.Error(w, "LocalRoute: invalid target", http.StatusBadGateway)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	status := http.StatusOK
	requestError := ""
	proxy.ModifyResponse = func(response *http.Response) error { status = response.StatusCode; return nil }
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		if route.PreserveHost {
			req.Host = r.Host
		} else {
			req.Host = target.Host
		}
		req.URL.Path = joinPath(route.PathPrefix, req.URL.Path)
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		status = http.StatusBadGateway
		requestError = err.Error()
		s.logger.Error("proxy request failed", "host", r.Host, "target", route.Target, "error", err)
		http.Error(w, fmt.Sprintf("LocalRoute: target %s is unavailable", route.Target), http.StatusBadGateway)
	}
	proxy.ServeHTTP(w, r)
	duration := time.Since(started)
	s.requests.Add(requestlog.Event{Time: started, Method: r.Method, Host: r.Host, Path: r.URL.Path, Target: route.Target, RouteID: route.ID, RuleID: route.RuleID, Status: status, DurationMS: duration.Milliseconds(), Error: requestError})
	s.logger.Info("request", "method", r.Method, "host", r.Host, "path", r.URL.Path, "target", route.Target, "rule", route.RuleID, "status", status, "duration", duration)
}

func joinPath(prefix, path string) string {
	if prefix == "" {
		return path
	}
	return strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(path, "/")
}

func IsAddressInUse(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) && strings.Contains(strings.ToLower(err.Error()), "address already in use")
}
