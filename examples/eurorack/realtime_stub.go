//go:build !linux && !darwin && !windows

package main

import "fmt"

// setRealtimePriority is not supported on this platform
func setRealtimePriority() error {
	return fmt.Errorf("real-time priority not supported on this platform")
}