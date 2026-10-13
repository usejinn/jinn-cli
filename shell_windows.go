package main

// notifyResize does nothing on Windows, which has no SIGWINCH: the shell
// keeps the size it started with.
func notifyResize(chan<- struct{}) (stop func()) { return func() {} }
