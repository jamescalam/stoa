//go:build darwin && cgo

package media

/*
#cgo CFLAGS: -x objective-c -Wno-deprecated-declarations
#cgo LDFLAGS: -framework MediaPlayer -framework Foundation -framework AppKit

#include <stdint.h>
#include <stdlib.h>

#import <AppKit/AppKit.h>
#import <MediaPlayer/MediaPlayer.h>

// Implemented in Go. All static functions below have internal linkage, which
// is what lets this preamble coexist with //export in one file.
extern void goMediaCmd(uintptr_t handle, int cmd);

typedef struct Bridge {
	id toggle, play, pause, next, prev, stop;
} Bridge;

// Opaque handle so the id-bearing struct never crosses into Go (cgo cannot
// size Objective-C object fields).
typedef void *BridgeRef;

static void initApp(void) {
	[NSApplication sharedApplication];
	[NSApp setActivationPolicy:NSApplicationActivationPolicyAccessory];
}

static void clearNowPlaying(void) {
	[MPNowPlayingInfoCenter defaultCenter].nowPlayingInfo = nil;
	[MPNowPlayingInfoCenter defaultCenter].playbackState = MPNowPlayingPlaybackStateStopped;
}

// cmd codes: 0 toggle, 1 play, 2 pause, 3 next, 4 prev, 5 stop.
static BridgeRef bridgeCreate(uintptr_t handle) {
	Bridge *b = (Bridge *)calloc(1, sizeof(Bridge));
	if (!b) {
		return NULL;
	}
	uintptr_t h = handle;
	MPRemoteCommandCenter *cc = [MPRemoteCommandCenter sharedCommandCenter];
	b->toggle = [cc.togglePlayPauseCommand addTargetWithHandler:^MPRemoteCommandHandlerStatus(MPRemoteCommandEvent *e) {
		goMediaCmd(h, 0);
		return MPRemoteCommandHandlerStatusSuccess;
	}];
	b->play = [cc.playCommand addTargetWithHandler:^MPRemoteCommandHandlerStatus(MPRemoteCommandEvent *e) {
		goMediaCmd(h, 1);
		return MPRemoteCommandHandlerStatusSuccess;
	}];
	b->pause = [cc.pauseCommand addTargetWithHandler:^MPRemoteCommandHandlerStatus(MPRemoteCommandEvent *e) {
		goMediaCmd(h, 2);
		return MPRemoteCommandHandlerStatusSuccess;
	}];
	b->next = [cc.nextTrackCommand addTargetWithHandler:^MPRemoteCommandHandlerStatus(MPRemoteCommandEvent *e) {
		goMediaCmd(h, 3);
		return MPRemoteCommandHandlerStatusSuccess;
	}];
	b->prev = [cc.previousTrackCommand addTargetWithHandler:^MPRemoteCommandHandlerStatus(MPRemoteCommandEvent *e) {
		goMediaCmd(h, 4);
		return MPRemoteCommandHandlerStatusSuccess;
	}];
	b->stop = [cc.stopCommand addTargetWithHandler:^MPRemoteCommandHandlerStatus(MPRemoteCommandEvent *e) {
		goMediaCmd(h, 5);
		return MPRemoteCommandHandlerStatusSuccess;
	}];
	cc.changePlaybackPositionCommand.enabled = NO; // no seek yet
	return (BridgeRef)b;
}

static void bridgeDestroy(BridgeRef ref) {
	Bridge *b = (Bridge *)ref;
	if (!b) {
		clearNowPlaying();
		return;
	}
	MPRemoteCommandCenter *cc = [MPRemoteCommandCenter sharedCommandCenter];
	if (b->toggle) [cc.togglePlayPauseCommand removeTarget:b->toggle];
	if (b->play)   [cc.playCommand removeTarget:b->play];
	if (b->pause)  [cc.pauseCommand removeTarget:b->pause];
	if (b->next)   [cc.nextTrackCommand removeTarget:b->next];
	if (b->prev)   [cc.previousTrackCommand removeTarget:b->prev];
	if (b->stop)   [cc.stopCommand removeTarget:b->stop];
	clearNowPlaying();
	free(b);
}

// state: 0 stopped, 1 playing, 2 paused.
static void updateNowPlaying(const char *title, const char *artist, const char *album,
                             double durationSecs, double elapsedSecs, int state) {
	@autoreleasepool {
		if (state == 0) {
			clearNowPlaying();
			return;
		}
		NSMutableDictionary *info = [NSMutableDictionary dictionary];
		if (title)  info[MPMediaItemPropertyTitle] = @(title);
		if (artist) info[MPMediaItemPropertyArtist] = @(artist);
		if (album)  info[MPMediaItemPropertyAlbumTitle] = @(album);
		if (durationSecs > 0) info[MPMediaItemPropertyPlaybackDuration] = @(durationSecs);
		if (elapsedSecs >= 0) info[MPNowPlayingInfoPropertyElapsedPlaybackTime] = @(elapsedSecs);
		info[MPNowPlayingInfoPropertyPlaybackRate] = @(state == 1 ? 1.0 : 0.0);

		[MPNowPlayingInfoCenter defaultCenter].nowPlayingInfo = info;
		[MPNowPlayingInfoCenter defaultCenter].playbackState = (state == 1)
			? MPNowPlayingPlaybackStatePlaying
			: MPNowPlayingPlaybackStatePaused;
	}
}

static void tickRunLoop(void) {
	[[NSRunLoop currentRunLoop] runMode:NSDefaultRunLoopMode
	                         beforeDate:[NSDate dateWithTimeIntervalSinceNow:0.05]];
}
*/
import "C"

import (
	"runtime/cgo"
	"sync"
	"unsafe"
)

type command int

const (
	cmdToggle command = iota
	cmdPlay
	cmdPause
	cmdNext
	cmdPrev
	cmdStop
)

// Service bridges stoa to the macOS MediaPlayer framework.
type Service struct {
	cmds    Commands
	handle  cgo.Handle
	cmdCh   chan command
	updates chan NowPlaying
	done    chan struct{}
	once    sync.Once
}

// New creates the service and starts the command dispatcher. The Cocoa
// integration itself is initialised later, by Run, on the main thread.
func New(cmds Commands) *Service {
	s := &Service{
		cmds:    cmds,
		cmdCh:   make(chan command, 16),
		updates: make(chan NowPlaying, 1),
		done:    make(chan struct{}),
	}
	s.handle = cgo.NewHandle(s)
	go s.dispatchCommands()
	return s
}

//export goMediaCmd
func goMediaCmd(handle C.uintptr_t, cmd C.int) {
	svc, ok := cgo.Handle(uintptr(handle)).Value().(*Service)
	if !ok || svc == nil {
		return
	}
	select {
	case svc.cmdCh <- command(cmd):
	default:
	}
}

func (s *Service) dispatchCommands() {
	for {
		select {
		case <-s.done:
			return
		case c := <-s.cmdCh:
			s.invoke(c)
		}
	}
}

func (s *Service) invoke(c command) {
	var f func()
	switch c {
	case cmdToggle:
		f = s.cmds.PlayPause
	case cmdPlay:
		f = s.cmds.Play
	case cmdPause:
		f = s.cmds.Pause
	case cmdNext:
		f = s.cmds.Next
	case cmdPrev:
		f = s.cmds.Prev
	case cmdStop:
		f = s.cmds.Stop
	}
	if f != nil {
		f()
	}
}

// Update publishes now-playing state (latest-wins; never blocks).
func (s *Service) Update(np NowPlaying) {
	select {
	case s.updates <- np:
	default:
		select {
		case <-s.updates:
		default:
		}
		select {
		case s.updates <- np:
		default:
		}
	}
}

// Run initialises the Cocoa run loop on the current goroutine — which must be
// locked to the main OS thread — and runs tui on a background goroutine,
// pumping the loop until tui returns.
func (s *Service) Run(tui func() error) error {
	resCh := make(chan error, 1)
	go func() { resCh <- tui() }()

	C.initApp()
	bridge := C.bridgeCreate(C.uintptr_t(s.handle))

	for {
		select {
		case np := <-s.updates:
			s.apply(np)
		default:
		}

		C.tickRunLoop()

		select {
		case err := <-resCh:
			C.bridgeDestroy(bridge)
			s.shutdown()
			return err
		default:
		}
	}
}

func (s *Service) apply(np NowPlaying) {
	var cTitle, cArtist, cAlbum *C.char
	if np.Title != "" {
		cTitle = C.CString(np.Title)
		defer C.free(unsafe.Pointer(cTitle))
	}
	if np.Artist != "" {
		cArtist = C.CString(np.Artist)
		defer C.free(unsafe.Pointer(cArtist))
	}
	if np.Album != "" {
		cAlbum = C.CString(np.Album)
		defer C.free(unsafe.Pointer(cAlbum))
	}
	state := C.int(2)
	if np.Playing {
		state = 1
	}
	C.updateNowPlaying(cTitle, cArtist, cAlbum,
		C.double(np.Duration.Seconds()), C.double(np.Elapsed.Seconds()), state)
}

// Close stops the dispatcher and releases the cgo handle. Safe to call twice.
func (s *Service) Close() { s.shutdown() }

func (s *Service) shutdown() {
	s.once.Do(func() {
		close(s.done)
		s.handle.Delete()
	})
}
