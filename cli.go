package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"localroute/internal/config"
	"localroute/internal/router"
	"localroute/internal/service"
)

func runCLI(args []string, stdout, stderr io.Writer, logger *slog.Logger) int {
	if len(args) == 0 {
		return -1
	}
	switch args[0] {
	case "gui":
		return -1
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "LocalRoute %s\n", version)
		return 0
	case "help", "--help", "-h":
		printUsage(stdout)
		return 0
	case "start":
		return runStart(args[1:], stderr, logger)
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "routes":
		return runRoutes(args[1:], stdout, stderr)
	case "_forward":
		if err := runPrivilegedForward(args[1:]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		printUsage(stderr)
		return 2
	}
}

func commandFlags(name string, args []string, stderr io.Writer) (*flag.FlagSet, *string, error) {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(stderr)
	defaultPath, err := resolveConfigPath("")
	if err != nil {
		return nil, nil, err
	}
	path := set.String("config", defaultPath, "configuration file path")
	if err := set.Parse(args); err != nil {
		return nil, nil, err
	}
	return set, path, nil
}

func runStart(args []string, stderr io.Writer, logger *slog.Logger) int {
	_, path, err := commandFlags("start", args, stderr)
	if err != nil {
		return 2
	}
	mgr := service.New(*path, logger)
	if err := mgr.Start(); err != nil {
		fmt.Fprintln(stderr, "start:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go mgr.Watch(ctx)
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := mgr.Stop(shutdownCtx); err != nil {
		fmt.Fprintln(stderr, "stop:", err)
		return 1
	}
	return 0
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	_, path, err := commandFlags("check", args, stderr)
	if err != nil {
		return 2
	}
	cfg, err := config.Load(*path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if _, err := router.New(cfg); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	rules := 0
	for _, route := range cfg.Routes {
		rules += len(route.Rules)
	}
	fmt.Fprintf(stdout, "configuration is valid: %s (%d routes, %d rules)\n", *path, len(cfg.Routes), rules)
	return 0
}

func runRoutes(args []string, stdout, stderr io.Writer) int {
	defaultPath, err := resolveConfigPath("")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	set := flag.NewFlagSet("routes", flag.ContinueOnError)
	set.SetOutput(stderr)
	path := set.String("config", defaultPath, "configuration file path")
	asJSON := set.Bool("json", false, "print routes as JSON")
	if err := set.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load(*path)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *asJSON {
		data, _ := json.MarshalIndent(cfg, "", "  ")
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	fmt.Fprintf(stdout, "%-28s %-48s %s\n", "NAME", "HOST", "TARGET")
	for _, route := range cfg.Routes {
		if route.Enabled {
			fmt.Fprintf(stdout, "%-28s %-48s %s\n", route.Name, route.Host, route.Target)
		}
	}
	return 0
}

func resolveConfigPath(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if value := os.Getenv("LOCALROUTE_CONFIG"); value != "" {
		return value, nil
	}
	if _, err := os.Stat(config.FileName); err == nil {
		return config.FileName, nil
	}
	return config.DefaultPath()
}

func resolveGUIConfigPath() (string, error) {
	if value := os.Getenv("LOCALROUTE_CONFIG"); value != "" {
		return value, nil
	}
	if _, err := os.Stat(config.FileName); err == nil {
		return config.FileName, nil
	}
	executable, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(executable)
		for range 8 {
			candidate := filepath.Join(dir, config.FileName)
			if _, statErr := os.Stat(candidate); statErr == nil {
				return candidate, nil
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return config.DefaultPath()
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, strings.TrimSpace(`LocalRoute - local development routing proxy

Usage:
  localroute              Open the desktop application
  localroute gui          Open the desktop application
  localroute start        Run the proxy in the foreground
  localroute check        Validate the configuration
  localroute routes       Print configured routes
  localroute version      Print version

Options:
  --config PATH           Use a specific configuration file`))
}
