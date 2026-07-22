//go:build !windows

package main

func parentExitSignal(uint32) <-chan struct{} {
	return make(chan struct{})
}
