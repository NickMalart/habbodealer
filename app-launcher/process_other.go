//go:build !windows
// +build !windows

package main

import "os/exec"

// runHiddenCmd is a no-op wrapper for non-Windows platforms.
func runHiddenCmd(cmd *exec.Cmd) error {
	if cmd == nil {
		return nil
	}
	return cmd.Run()
}
