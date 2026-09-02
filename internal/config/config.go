package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	CurrentVersion = 2
	FileName       = "localroute.yml"
)

var validID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

type Config struct {
	Version  int      `yaml:"version" json:"version"`
	Listener Listener `yaml:"listener" json:"listener"`
	Routes   []Route  `yaml:"routes" json:"routes"`
}
type Listener struct {
	Address string `yaml:"address" json:"address"`
	Port    int    `yaml:"port" json:"port"`
}

func (l Listener) String() string { return net.JoinHostPort(l.Address, fmt.Sprint(l.Port)) }

type Route struct {
	ID           string `yaml:"id" json:"id"`
	Name         string `yaml:"name" json:"name"`
	Group        string `yaml:"group,omitempty" json:"group,omitempty"`
	Enabled      bool   `yaml:"enabled" json:"enabled"`
	Host         string `yaml:"host" json:"host"`
	Target       string `yaml:"target" json:"target"`
	PreserveHost bool   `yaml:"preserveHost,omitempty" json:"preserveHost,omitempty"`
	Rules        []Rule `yaml:"rules,omitempty" json:"rules,omitempty"`
}
type Rule struct {
	ID         string `yaml:"id" json:"id"`
	Name       string `yaml:"name,omitempty" json:"name,omitempty"`
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	Priority   int    `yaml:"priority,omitempty" json:"priority,omitempty"`
	Match      Match  `yaml:"match" json:"match"`
	Target     string `yaml:"target" json:"target"`
	PathPrefix string `yaml:"pathPrefix,omitempty" json:"pathPrefix,omitempty"`
}
type Match struct {
	Methods    []string `yaml:"methods,omitempty" json:"methods,omitempty"`
	Path       string   `yaml:"path,omitempty" json:"path,omitempty"`
	PathPrefix string   `yaml:"pathPrefix,omitempty" json:"pathPrefix,omitempty"`
}

func Default() Config {
	return Config{Version: CurrentVersion, Listener: Listener{Address: "127.0.0.1", Port: 80}, Routes: []Route{}}
}
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}
	return filepath.Join(dir, "LocalRoute", FileName), nil
}
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg Config
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, fmt.Errorf("validate %s: %w", path, err)
	}
	return cfg, nil
}
func Save(path string, cfg Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encode config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".localroute-*.yml")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

func (c Config) Validate() error {
	var problems []error
	if c.Version != CurrentVersion {
		problems = append(problems, fmt.Errorf("unsupported version %d; expected %d", c.Version, CurrentVersion))
	}
	if net.ParseIP(c.Listener.Address) == nil && c.Listener.Address != "localhost" {
		problems = append(problems, fmt.Errorf("listener.address %q is not an IP address or localhost", c.Listener.Address))
	}
	if c.Listener.Port < 1 || c.Listener.Port > 65535 {
		problems = append(problems, fmt.Errorf("listener.port %d is outside 1-65535", c.Listener.Port))
	}
	ids, hosts := map[string]bool{}, map[string]bool{}
	for i, route := range c.Routes {
		prefix := fmt.Sprintf("routes[%d]", i)
		if !validID.MatchString(route.ID) {
			problems = append(problems, fmt.Errorf("%s.id %q must use lowercase letters, numbers, _ or -", prefix, route.ID))
		}
		if ids[route.ID] {
			problems = append(problems, fmt.Errorf("duplicate route id %q", route.ID))
		}
		ids[route.ID] = true
		if strings.TrimSpace(route.Name) == "" {
			problems = append(problems, fmt.Errorf("%s.name is empty", prefix))
		}
		host := strings.ToLower(strings.TrimSpace(route.Host))
		if host == "" {
			problems = append(problems, fmt.Errorf("%s.host is empty", prefix))
		}
		if route.Enabled && hosts[host] {
			problems = append(problems, fmt.Errorf("duplicate enabled host %q", route.Host))
		}
		if route.Enabled {
			hosts[host] = true
		}
		if _, err := NormalizeTarget(route.Target); err != nil {
			problems = append(problems, fmt.Errorf("%s.target: %w", prefix, err))
		}
		ruleIDs := map[string]bool{}
		for j, rule := range route.Rules {
			rp := fmt.Sprintf("%s.rules[%d]", prefix, j)
			if !validID.MatchString(rule.ID) {
				problems = append(problems, fmt.Errorf("%s.id %q is invalid", rp, rule.ID))
			}
			if ruleIDs[rule.ID] {
				problems = append(problems, fmt.Errorf("duplicate rule id %q in route %q", rule.ID, route.ID))
			}
			ruleIDs[rule.ID] = true
			matches := 0
			if rule.Match.Path != "" {
				matches++
			}
			if rule.Match.PathPrefix != "" {
				matches++
			}
			if matches != 1 {
				problems = append(problems, fmt.Errorf("%s.match must define exactly one of path or pathPrefix", rp))
			}
			for _, m := range rule.Match.Methods {
				if m != strings.ToUpper(m) {
					problems = append(problems, fmt.Errorf("%s.match.methods must be uppercase", rp))
				}
			}
			if _, err := NormalizeTarget(rule.Target); err != nil {
				problems = append(problems, fmt.Errorf("%s.target: %w", rp, err))
			}
		}
	}
	return errors.Join(problems...)
}
func NormalizeTarget(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid HTTP target %q", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme %q", u.Scheme)
	}
	return u.String(), nil
}
