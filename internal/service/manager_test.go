package service

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"localroute/internal/config"
)

func TestStartReportsOccupiedPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	cfg := config.Default()
	cfg.Listener = config.Listener{Address: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port}
	path := filepath.Join(t.TempDir(), "config.yml")
	if err := config.Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	manager := New(path, nil)
	defer manager.Stop(context.Background())
	if err := manager.Start(); err == nil {
		t.Fatal("start succeeded on an occupied port")
	}
	status := manager.Status()
	if status.Running || status.LastError == "" {
		t.Fatalf("incorrect status: %+v", status)
	}
}
