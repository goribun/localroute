package router

import (
	"localroute/internal/config"
	"testing"
)

func TestMatchPriorityMethodAndBoundary(t *testing.T) {
	t.Parallel()
	cfg := config.Config{Version: 2, Listener: config.Listener{Address: "127.0.0.1", Port: 8080}, Routes: []config.Route{{ID: "web", Name: "Web", Enabled: true, Host: "web.test", Target: "localhost:3000", Rules: []config.Rule{{ID: "api", Enabled: true, Priority: 100, Match: config.Match{Methods: []string{"GET"}, PathPrefix: "/api"}, Target: "localhost:9000", PathPrefix: "/gateway"}}}}}
	table, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := table.Match("WEB.TEST:8080", "GET", "/api/users")
	if !ok || got.RuleID != "api" || got.PathPrefix != "/gateway" {
		t.Fatalf("match=%#v,%v", got, ok)
	}
	got, ok = table.Match("web.test", "POST", "/api/users")
	if !ok || got.RuleID != "" {
		t.Fatalf("method fallback=%#v,%v", got, ok)
	}
	got, _ = table.Match("web.test", "GET", "/api-docs")
	if got.RuleID != "" {
		t.Fatalf("boundary=%#v", got)
	}
}
func TestDisabledRouteNotMatched(t *testing.T) {
	t.Parallel()
	table, err := New(config.Config{Version: 2, Listener: config.Listener{Address: "127.0.0.1", Port: 8080}, Routes: []config.Route{{ID: "off", Name: "Off", Host: "off.test", Target: "localhost:9000"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := table.Match("off.test", "GET", "/"); ok {
		t.Fatal("disabled route matched")
	}
}
