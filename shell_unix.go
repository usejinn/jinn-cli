//go:build !windows

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// notifyResize tells resized when the terminal's size changes.
func notifyResize(resized chan<- struct{}) (stop func()) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-sig:
				select {
				case resized <- struct{}{}:
				default:
				}
			case <-done:
				return
			}
		}
	}()
	return func() { signal.Stop(sig); close(done) }
}
