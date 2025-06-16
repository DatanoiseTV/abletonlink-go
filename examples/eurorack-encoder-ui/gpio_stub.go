//go:build !linux

package main

import (
	"fmt"
)

// Stub GPIO types for non-Linux platforms
type Line struct{}
type Chip struct{}

// Stub GPIO functions for non-Linux platforms
func NewChip(name string) (*Chip, error) {
	return nil, fmt.Errorf("GPIO not supported on this platform - use -dry-run flag")
}

func (c *Chip) RequestLine(pin int, options ...interface{}) (*Line, error) {
	return nil, fmt.Errorf("GPIO not supported on this platform - use -dry-run flag")
}

func (c *Chip) Close() error {
	return nil
}

func (l *Line) SetValue(value int) error {
	return nil
}

func (l *Line) Close() error {
	return nil
}

func (l *Line) Reconfigure(options ...interface{}) error {
	return nil
}

// Stub constants
const (
	AsInput       = 0
	AsOutput      = func(int) int { return 0 }
	WithPullDown  = 0
	WithEventHandler = func(interface{}) int { return 0 }
)

// Stub event types
type LineEvent struct {
	Type      int
	Timestamp time.Time
}

type LineEventHandler func(LineEvent)

func NewLineEventHandler(handler func(LineEvent)) LineEventHandler {
	return handler
}

const (
	LineEventRisingEdge = 1
)