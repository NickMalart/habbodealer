//go:build windows
// +build windows

package main

import (
	"os/exec"
	"syscall"
)

// runHiddenCmd runs the given command while hiding the console window on Windows.
func runHiddenCmd(cmd *exec.Cmd) error {
	if cmd == nil {
		return nil
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	return cmd.Run()
}
