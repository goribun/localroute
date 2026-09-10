package proxy

import (
	"context"
	"io"
	"localroute/internal/config"
	"localroute/internal/requestlog"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestServeHTTPRoutesAndRecords(t *testing.T) {
	t.Parallel()
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prefix/api/users" {
			t.Errorf("path=%q", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer backend.Close()
	cfg := config.Config{Version: 2, Listener: config.Listener{Address: "127.0.0.1", Port: 8080}, Routes: []config.Route{{ID: "front", Name: "Front", Enabled: true, Host: "front.test", Target: backend.URL, Rules: []config.Rule{{ID: "api", Enabled: true, Match: config.Match{PathPrefix: "/api"}, Target: backend.URL, PathPrefix: "/prefix"}}}}}
	store := requestlog.New(10)
	server, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), store)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "http://front.test/api/users", nil)
	req.Host = "front.test"
	res := httptest.NewRecorder()
	server.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent {
		t.Fatalf("status=%d", res.Code)
	}
	events := store.List()
	if len(events) != 1 || events[0].RuleID != "api" || events[0].Status != 204 {
		t.Fatalf("events=%#v", events)
	}
}
func TestUnknownHost(t *testing.T) {
	t.Parallel()
	server, err := New(config.Default(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	res := httptest.NewRecorder()
	server.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "http://unknown.test/", nil))
	if res.Code != http.StatusNotFound {
		t.Fatalf("status=%d", res.Code)
	}
}

// A wake must evict both browser and backend keep-alive sockets while leaving
// the listener available for the next request.
func TestResetConnectionsKeepsListenerAndReconnects(t *testing.T) {
	backendPeers := make(chan string, 3)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendPeers <- r.RemoteAddr
		_, _ = io.WriteString(w, "ok")
	}))
	defer backend.Close()
	cfg := config.Config{Version: 2, Listener: config.Listener{Address: "127.0.0.1", Port: 8080}, Routes: []config.Route{{ID: "front", Enabled: true, Host: "front.test", Target: backend.URL}}}
	server, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())
	clientTransport := http.DefaultTransport.(*http.Transport).Clone()
	defer clientTransport.CloseIdleConnections()
	client := &http.Client{Transport: clientTransport, Timeout: 2 * time.Second}
	request := func() string {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, "http://"+listener.Addr().String(), nil)
		req.Host = "front.test"
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("status=%d", res.StatusCode)
		}
		return <-backendPeers
	}
	before := request()
	if reused := request(); reused != before {
		t.Fatal("test did not establish backend connection reuse")
	}
	server.ResetConnections()
	if after := request(); after == before {
		t.Fatal("wake reused the old backend socket")
	}
}

func TestResetConnectionsCancelsInFlightRequest(t *testing.T) {
	entered, cancelled := make(chan struct{}), make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-r.Context().Done()
		close(cancelled)
	}))
	defer backend.Close()
	cfg := config.Config{Version: 2, Listener: config.Listener{Address: "127.0.0.1", Port: 8080}, Routes: []config.Route{{ID: "front", Enabled: true, Host: "front.test", Target: backend.URL}}}
	server, err := New(cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve(listener)
	defer server.Shutdown(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		req, _ := http.NewRequest(http.MethodGet, "http://"+listener.Addr().String(), nil)
		req.Host = "front.test"
		client := &http.Client{Timeout: 2 * time.Second}
		if res, err := client.Do(req); err == nil {
			res.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("backend never received request")
	}
	server.ResetConnections()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("old request was not cancelled")
	}
	<-done
}
