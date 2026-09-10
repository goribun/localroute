//go:build darwin

package main

import (
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestForwardChild(t *testing.T) {
	if os.Getenv("LOCALROUTE_TEST_FORWARD") != "1" {
		return
	}
	if err := runPrivilegedForward(strings.Split(os.Getenv("LOCALROUTE_TEST_FORWARD_ARGS"), "\n")); err != nil {
		t.Fatal(err)
	}
}

func TestForwardWakeResetPreservesListener(t *testing.T) {
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	go func() {
		for {
			conn, err := backend.Accept()
			if err != nil {
				return
			}
			go func() { defer conn.Close(); _, _ = io.Copy(conn, conn) }()
		}
	}()
	port, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := port.Addr().String()
	port.Close()
	pidFile := filepath.Join(t.TempDir(), "forward.pid")
	child := exec.Command(os.Args[0], "-test.run=^TestForwardChild$")
	child.Env = append(os.Environ(), "LOCALROUTE_TEST_FORWARD=1", "LOCALROUTE_TEST_FORWARD_ARGS="+strings.Join([]string{"--listen", listen, "--target", backend.Addr().String(), "--uid", strconv.Itoa(os.Getuid()), "--pid-file", pidFile}, "\n"))
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = child.Process.Kill(); _ = child.Wait() }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(pidFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("forwarder did not become ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	connect := func() net.Conn {
		t.Helper()
		conn, err := net.DialTimeout("tcp", listen, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := conn.Write([]byte("ping")); err != nil {
			t.Fatal(err)
		}
		data := make([]byte, 4)
		if _, err := io.ReadFull(conn, data); err != nil {
			t.Fatal(err)
		}
		if string(data) != "ping" {
			t.Fatalf("echo=%q", data)
		}
		return conn
	}
	before := connect()
	if err := child.Process.Signal(syscall.SIGUSR1); err != nil {
		t.Fatal(err)
	}
	if _, err := before.Read(make([]byte, 1)); err == nil {
		t.Fatal("old connection survived wake reset")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("wake reset did not close the old connection")
	}
	connect()
	if err := child.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatalf("forwarder exited: %v", err)
	}
}
