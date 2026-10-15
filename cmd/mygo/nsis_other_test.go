//go:build !windows

package main

import (
	"errors"
	"os/exec"
)

// runUninstaller runs an uninstaller; only Windows has them.
func runUninstaller(cmd *exec.Cmd) ([]byte, error) { return cmd.CombinedOutput() }

func holdImage(string) (func(), error) { return nil, errors.New("only Windows maps programs so") }
