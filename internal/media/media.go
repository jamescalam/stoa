// Package media integrates stoa with the operating system's media controls.
// On macOS this publishes now-playing metadata to Control Center and the lock
// screen and receives remote commands from the media keys, AirPods and
// headphones. On other platforms (or without cgo) it is a no-op.
package media

import "time"

// NowPlaying is the metadata published to the system.
type NowPlaying struct {
	Title    string
	Artist   string
	Album    string
	Duration time.Duration
	Elapsed  time.Duration
	Playing  bool
}

// Commands are invoked when the OS delivers a remote-control event. Any nil
// field is ignored. Implementations may be called from an OS thread, so the
// callbacks must be safe to invoke from any goroutine.
type Commands struct {
	PlayPause func()
	Play      func()
	Pause     func()
	Next      func()
	Prev      func()
	Stop      func()
}
