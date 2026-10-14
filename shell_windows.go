package main

import "time"

// notifyResize tells resized every quarter second: Windows has no SIGWINCH,
// so the shell reads the size that often and sends it when it changed.
func notifyResize(resized chan<- struct{}) (stop func()) {
	t := time.NewTicker(250 * time.Millisecond)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-t.C:
				select {
				case resized <- struct{}{}:
				default:
				}
			case <-done:
				return
			}
		}
	}()
	return func() { t.Stop(); close(done) }
}
