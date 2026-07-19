//go:build windows

package main

import "golang.org/x/sys/windows"

func parentExitSignal(pid uint32) <-chan struct{} {
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		const synchronize = uint32(0x00100000)
		handle, err := windows.OpenProcess(synchronize, false, pid)
		if err != nil {
			return
		}
		defer windows.CloseHandle(handle)
		_, _ = windows.WaitForSingleObject(handle, windows.INFINITE)
	}()
	return stopped
}
