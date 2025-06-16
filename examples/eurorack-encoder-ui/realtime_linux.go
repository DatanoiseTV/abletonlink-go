//go:build linux

package main

import (
	"syscall"
	"unsafe"
)

const (
	SCHED_FIFO = 1
	SCHED_RR   = 2
)

type schedParam struct {
	priority int32
}

// setRealtimePriority sets real-time priority on Linux using SCHED_FIFO
func setRealtimePriority() error {
	// Set SCHED_FIFO with high priority (80 out of 99)
	param := schedParam{priority: 80}
	
	_, _, errno := syscall.Syscall(
		syscall.SYS_SCHED_SETSCHEDULER,
		uintptr(0), // current process
		uintptr(SCHED_FIFO),
		uintptr(unsafe.Pointer(&param)),
	)
	
	if errno != 0 {
		return errno
	}
	
	return nil
}