package router

import (
	"fmt"
	"localroute/internal/config"
	"net"
	"net/url"
	"slices"
	"strings"
)

type Route struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	Target       string `json:"target"`
	PathPrefix   string `json:"pathPrefix,omitempty"`
	RuleID       string `json:"ruleId,omitempty"`
	PreserveHost bool   `json:"preserveHost"`
}
type Table struct {
	byHost map[string]Route
	rules  map[string][]compiledRule
}
type compiledRule struct {
	priority, order  int
	methods          []string
	path, pathPrefix string
	route            Route
}

func New(cfg config.Config) (*Table, error) {
	t := &Table{byHost: map[string]Route{}, rules: map[string][]compiledRule{}}
	for _, item := range cfg.Routes {
		if !item.Enabled {
			continue
		}
		target, err := config.NormalizeTarget(item.Target)
		if err != nil {
			return nil, fmt.Errorf("route %q: %w", item.ID, err)
		}
		host := normalizeHost(item.Host)
		t.byHost[host] = Route{ID: item.ID, Name: item.Name, Host: item.Host, Target: target, PreserveHost: item.PreserveHost}
		for order, rule := range item.Rules {
			if !rule.Enabled {
				continue
			}
			ruleTarget, err := config.NormalizeTarget(rule.Target)
			if err != nil {
				return nil, err
			}
			t.rules[host] = append(t.rules[host], compiledRule{priority: rule.Priority, order: order, methods: rule.Match.Methods, path: rule.Match.Path, pathPrefix: rule.Match.PathPrefix, route: Route{ID: item.ID, Name: item.Name, Host: item.Host, Target: ruleTarget, PathPrefix: rule.PathPrefix, RuleID: rule.ID, PreserveHost: item.PreserveHost}})
		}
		slices.SortStableFunc(t.rules[host], func(a, b compiledRule) int {
			if a.priority != b.priority {
				return b.priority - a.priority
			}
			return a.order - b.order
		})
	}
	return t, nil
}
func (t *Table) Match(host, method, path string) (Route, bool) {
	host = normalizeHost(host)
	for _, rule := range t.rules[host] {
		if len(rule.methods) > 0 && !slices.Contains(rule.methods, method) {
			continue
		}
		exact := rule.path != "" && path == rule.path
		prefix := rule.pathPrefix != "" && (path == rule.pathPrefix || strings.HasPrefix(path, strings.TrimSuffix(rule.pathPrefix, "/")+"/"))
		if exact || prefix {
			return rule.route, true
		}
	}
	route, ok := t.byHost[host]
	return route, ok
}
func normalizeHost(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		hostport = host
	}
	return strings.ToLower(strings.TrimSuffix(hostport, "."))
}
func ParseTarget(raw string) (*url.URL, error) {
	normalized, err := config.NormalizeTarget(raw)
	if err != nil {
		return nil, err
	}
	return url.Parse(normalized)
}
