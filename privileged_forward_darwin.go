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

func requiresPrivilegedPortForward(port int) bool { return port < 1024 }

func startPrivilegedPortForward(listen, target string) (int, error) {
	executable, err := os.Executable()
	if err != nil {
		return 0, err
	}
	pidFile := privilegedForwardPIDFile()
	if err := stopExistingPrivilegedPortForward(pidFile); err != nil {
		return 0, fmt.Errorf("stop previous privileged port forward: %w", err)
	}
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
	return process.Signal(syscall.SIGTERM)
}

func privilegedForwardPIDFile() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("localroute-forward-%d.pid", os.Getuid()))
}

func cleanupPrivilegedPortForward() error {
	return stopExistingPrivilegedPortForward(privilegedForwardPIDFile())
}

func stopExistingPrivilegedPortForward(pidFile string) error {
	data, err := os.ReadFile(pidFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		_ = os.Remove(pidFile)
		return nil
	}
	command, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		_ = os.Remove(pidFile)
		return nil
	}
	commandLine := string(command)
	if !strings.Contains(commandLine, " _forward ") || !strings.Contains(commandLine, "--pid-file "+pidFile) {
		return fmt.Errorf("pid file points to a process that is not a LocalRoute forwarder (pid %d)", pid)
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	if waitForProcessExit(pid, time.Second) {
		_ = os.Remove(pidFile)
		return nil
	}
	if err := process.Signal(syscall.SIGKILL); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	if waitForProcessExit(pid, time.Second) {
		_ = os.Remove(pidFile)
		return nil
	}
	return fmt.Errorf("forwarder pid %d did not stop", pid)
}

func waitForProcessExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		state, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "state=").Output()
		if err != nil || strings.HasPrefix(strings.TrimSpace(string(state)), "Z") {
			return true
		}
		time.Sleep(25 * time.Millisecond)
	}
	return false
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
