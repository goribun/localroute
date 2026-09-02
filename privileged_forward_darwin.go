//go:build darwin

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func startPrivilegedPortForward(listen, target string) (int, error) {
	executable, err := os.Executable()
	if err != nil {
		return 0, err
	}
	pidFile := filepath.Join(os.TempDir(), fmt.Sprintf("localroute-forward-%d.pid", os.Getuid()))
	_ = os.Remove(pidFile)
	command := strings.Join([]string{shellQuote(executable), "_forward", "--listen", shellQuote(listen), "--target", shellQuote(target), "--uid", strconv.Itoa(os.Getuid()), "--pid-file", shellQuote(pidFile), ">/tmp/localroute-forward.log 2>&1 &"}, " ")
	authorize := exec.Command("osascript",
		"-e", "on run argv",
		"-e", "do shell script (item 1 of argv) with administrator privileges",
		"-e", "end run",
		command,
	)
	if output, err := authorize.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("administrator authorization failed: %s: %w", strings.TrimSpace(string(output)), err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(pidFile)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr == nil {
				return pid, nil
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0, errors.New("privileged port forward did not start; see /tmp/localroute-forward.log")
}

func stopPrivilegedPortForward(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(os.Interrupt)
}

func runPrivilegedForward(args []string) error {
	set := flag.NewFlagSet("_forward", flag.ContinueOnError)
	listen := set.String("listen", "", "")
	target := set.String("target", "", "")
	uid := set.Int("uid", -1, "")
	pidFile := set.String("pid-file", "", "")
	if err := set.Parse(args); err != nil {
		return err
	}
	if *listen == "" || *target == "" || *uid < 0 || *pidFile == "" {
		return errors.New("invalid forward arguments")
	}
	listener, err := net.Listen("tcp", *listen)
	if err != nil {
		return fmt.Errorf("listen %s: %w", *listen, err)
	}
	if err := syscall.Setuid(*uid); err != nil {
		listener.Close()
		return fmt.Errorf("drop privileges: %w", err)
	}
	if err := os.WriteFile(*pidFile, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		listener.Close()
		return err
	}
	defer os.Remove(*pidFile)
	for {
		client, err := listener.Accept()
		if err != nil {
			return err
		}
		go bridgeConnection(client, *target)
	}
}

func bridgeConnection(client net.Conn, target string) {
	defer client.Close()
	upstream, err := net.DialTimeout("tcp", target, 3*time.Second)
	if err != nil {
		return
	}
	defer upstream.Close()
	go func() { _, _ = io.Copy(upstream, client); _ = upstream.(*net.TCPConn).CloseWrite() }()
	_, _ = io.Copy(client, upstream)
}

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
