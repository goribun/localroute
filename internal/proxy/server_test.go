package proxy

import (
	"io"
	"localroute/internal/config"
	"localroute/internal/requestlog"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
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
