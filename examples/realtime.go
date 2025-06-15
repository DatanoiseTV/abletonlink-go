package main

import (
	"fmt"
	"os"
	"runtime"
)

// Platform-specific implementations are in separate files with build tags
// This is just a placeholder - the actual implementations are in:
// - realtime_linux.go
// - realtime_darwin.go  
// - realtime_windows.go

// logRealtimePriorityInfo logs information about real-time priority setup
func logRealtimePriorityInfo() {
	switch runtime.GOOS {
	case "linux":
		fmt.Fprintf(os.Stderr, "Real-time priority enabled. On Linux, you may need to:\n")
		fmt.Fprintf(os.Stderr, "  1. Add your user to the 'audio' group: sudo usermod -a -G audio $USER\n")
		fmt.Fprintf(os.Stderr, "  2. Configure limits in /etc/security/limits.conf:\n")
		fmt.Fprintf(os.Stderr, "     @audio - rtprio 99\n")
		fmt.Fprintf(os.Stderr, "     @audio - memlock unlimited\n")
		fmt.Fprintf(os.Stderr, "  3. Log out and back in for changes to take effect\n\n")
	case "darwin":
		fmt.Fprintf(os.Stderr, "Real-time priority enabled. On macOS, this may require running as root or with appropriate entitlements.\n\n")
	case "windows":
		fmt.Fprintf(os.Stderr, "Real-time priority enabled. On Windows, this may require administrator privileges.\n\n")
	}
}