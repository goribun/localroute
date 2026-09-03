//go:build !darwin

package main

import "errors"

func requiresPrivilegedPortForward(int) bool { return false }
func startPrivilegedPortForward(string, string) (int, error) {
	return 0, errors.New("privileged ports are not supported on this platform yet")
}
func stopPrivilegedPortForward(int) error { return nil }
func cleanupPrivilegedPortForward() error { return nil }
func runPrivilegedForward([]string) error { return errors.New("privileged forwarding is unavailable") }
