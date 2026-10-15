//go:build windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"syscall"
	"unsafe"
)

// runUninstaller runs an NSIS uninstaller, and returns its output once the
// uninstall is over. The uninstaller does not uninstall: it copies itself
// into the temporary directory, starts the copy, which can remove the
// app's folder with the uninstaller in it, and exits at once. So this
// waits for the processes it started too.
func runUninstaller(cmd *exec.Cmd) ([]byte, error) {
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// Held, so that no other process gets the uninstaller's id until the
	// copy it started is found: until Wait, exec holds it.
	const processQueryLimitedInformation = 0x1000
	root, err := syscall.OpenProcess(syscall.SYNCHRONIZE|processQueryLimitedInformation, false, uint32(cmd.Process.Pid))
	if err != nil {
		cmd.Wait()
		return out.Bytes(), fmt.Errorf("holding the uninstaller: %w", err)
	}
	defer syscall.CloseHandle(root)
	if err := cmd.Wait(); err != nil {
		return out.Bytes(), err
	}
	pid := uint32(cmd.Process.Pid)
	// A process found may exit, and its id go to another, before it is
	// opened: those still started by the uninstaller once held are the
	// ones found.
	var held []syscall.Handle
	var ids []uint32
	defer func() {
		for _, h := range held {
			syscall.CloseHandle(h)
		}
	}()
	for _, id := range processTree(pid)[1:] {
		if h, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, id); err == nil {
			held = append(held, h)
			ids = append(ids, id)
		}
	}
	tree := processTree(pid)
	for i, h := range held {
		if !slices.Contains(tree, ids[i]) {
			continue
		}
		// A minute only guards against a hang.
		switch ev, err := syscall.WaitForSingleObject(h, 60_000); {
		case err != nil:
			return out.Bytes(), err
		case ev != syscall.WAIT_OBJECT_0:
			return out.Bytes(), errors.New("the uninstaller's copy did not end in a minute")
		}
	}
	return out.Bytes(), nil
}

var procLoadLibraryExW = syscall.NewLazyDLL("kernel32.dll").NewProc("LoadLibraryExW")

// holdImage maps the program at path as Windows maps a running program,
// which it then refuses to delete, and returns what unmaps it.
func holdImage(path string) (release func(), err error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	const loadLibraryAsImageResource = 0x20
	h, _, err := procLoadLibraryExW.Call(uintptr(unsafe.Pointer(p)), 0, loadLibraryAsImageResource)
	if h == 0 {
		return nil, fmt.Errorf("mapping %s: %w", path, err)
	}
	return func() { syscall.FreeLibrary(syscall.Handle(h)) }, nil
}
