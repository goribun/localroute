package main

import (
	"embed"
	"fmt"
	"log/slog"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

const version = "0.1.0-beta"

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	if code := runCLI(os.Args[1:], os.Stdout, os.Stderr, logger); code >= 0 {
		os.Exit(code)
	}
	configPath, err := resolveGUIConfigPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	app := NewApp(configPath, logger)
	if err := wails.Run(&options.App{
		Title: "LocalRoute", Width: 1120, Height: 720, MinWidth: 820, MinHeight: 560,
		AssetServer:      &assetserver.Options{Assets: assets, Handler: app},
		BackgroundColour: &options.RGBA{R: 245, G: 247, B: 250, A: 1},
		OnStartup:        app.startup, OnShutdown: app.shutdown,
		HideWindowOnClose:  true,
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: "com.localroute.desktop", OnSecondInstanceLaunch: func(options.SecondInstanceData) { app.Show() }},
	}); err != nil {
		logger.Error("application stopped", "error", err)
		os.Exit(1)
	}
}
