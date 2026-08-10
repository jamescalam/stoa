//go:build darwin && cgo

package main

import "runtime"

// The macOS MediaPlayer / AppKit run loop must run on the process's main OS
// thread. Locking the main goroutine here keeps it on thread 0 so media.Run
// can pump the Cocoa run loop while Bubble Tea runs on a background goroutine.
func init() {
	runtime.LockOSThread()
}
