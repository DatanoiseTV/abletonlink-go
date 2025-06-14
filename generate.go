//go:build ignore

package main

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func main() {
	// Get the directory where this source file is located
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)

	// Run the build script
	buildScript := filepath.Join(dir, "build.sh")
	
	// Check if the script exists
	if _, err := os.Stat(buildScript); os.IsNotExist(err) {
		log.Fatalf("Build script not found: %s", buildScript)
	}

	cmd := exec.Command("/bin/bash", buildScript)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		log.Fatalf("Failed to build Ableton Link: %v", err)
	}
}