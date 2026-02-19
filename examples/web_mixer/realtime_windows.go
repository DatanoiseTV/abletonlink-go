//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	REALTIME_PRIORITY_CLASS = 0x00000100
	HIGH_PRIORITY_CLASS     = 0x00000080
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrentProcess   = kernel32.NewProc("GetCurrentProcess")
	procSetPriorityClass    = kernel32.NewProc("SetPriorityClass")
)

// setRealtimePriority sets real-time priority on Windows
func setRealtimePriority() error {
	handle, _, _ := procGetCurrentProcess.Call()
	
	// Try to set REALTIME_PRIORITY_CLASS first
	success, _, err := procSetPriorityClass.Call(
		handle,
		uintptr(REALTIME_PRIORITY_CLASS),
	)
	
	if success == 0 {
		// If realtime fails, try HIGH_PRIORITY_CLASS
		success, _, err = procSetPriorityClass.Call(
			handle,
			uintptr(HIGH_PRIORITY_CLASS),
		)
		
		if success == 0 {
			return fmt.Errorf("failed to set high priority on Windows: %v", err)
		}
		
		// Log that we fell back to high priority
		fmt.Printf("Note: Set to HIGH priority (realtime requires admin privileges)\n")
		return nil
	}
	
	return nil
}