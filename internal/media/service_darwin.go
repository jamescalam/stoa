//go:build darwin && cgo

package media

/*
#cgo CFLAGS: -x objective-c -Wno-deprecated-declarations
#cgo LDFLAGS: -framework MediaPlayer -framework Foundation -framework AppKit -framework CoreAudio

#include <stdint.h>
#include <stdlib.h>

#import <AppKit/AppKit.h>
#import <MediaPlayer/MediaPlayer.h>
#import <CoreAudio/CoreAudio.h>

// Older SDKs spell the master element differently.
#ifndef kAudioObjectPropertyElementMain
#define kAudioObjectPropertyElementMain kAudioObjectPropertyElementMaster
#endif

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

// DevInfo carries the current output device's name, its active data-source name
// (e.g. "Headphones" on built-in output) and its transport type FourCC. name
// and source are malloc'd and owned by the caller (may be NULL).
typedef struct DevInfo {
	char *name;
	char *source;
	uint32_t transport;
} DevInfo;

static char *cfCopy(CFStringRef s) {
	if (!s) return NULL;
	CFIndex max = CFStringGetMaximumSizeForEncoding(CFStringGetLength(s), kCFStringEncodingUTF8) + 1;
	char *buf = (char *)malloc(max);
	if (!buf) return NULL;
	if (!CFStringGetCString(s, buf, max, kCFStringEncodingUTF8)) {
		free(buf);
		return NULL;
	}
	return buf;
}

// currentOutputDevice reads the system default output device's name, transport
// type and active output data-source name via CoreAudio.
static DevInfo currentOutputDevice(void) {
	DevInfo di = {NULL, NULL, 0};

	AudioObjectPropertyAddress addr = {
		kAudioHardwarePropertyDefaultOutputDevice,
		kAudioObjectPropertyScopeGlobal,
		kAudioObjectPropertyElementMain,
	};
	AudioObjectID dev = kAudioObjectUnknown;
	UInt32 sz = sizeof(dev);
	if (AudioObjectGetPropertyData(kAudioObjectSystemObject, &addr, 0, NULL, &sz, &dev) != noErr ||
	    dev == kAudioObjectUnknown) {
		return di;
	}

	CFStringRef name = NULL;
	sz = sizeof(name);
	addr.mSelector = kAudioObjectPropertyName;
	if (AudioObjectGetPropertyData(dev, &addr, 0, NULL, &sz, &name) == noErr && name) {
		di.name = cfCopy(name);
		CFRelease(name);
	}

	UInt32 transport = 0;
	sz = sizeof(transport);
	addr.mSelector = kAudioDevicePropertyTransportType;
	if (AudioObjectGetPropertyData(dev, &addr, 0, NULL, &sz, &transport) == noErr) {
		di.transport = transport;
	}

	// The active data source distinguishes built-in speakers from the headphone
	// jack; it isn't present on every device.
	AudioObjectPropertyAddress srcAddr = {
		kAudioDevicePropertyDataSource,
		kAudioDevicePropertyScopeOutput,
		kAudioObjectPropertyElementMain,
	};
	UInt32 srcID = 0;
	sz = sizeof(srcID);
	if (AudioObjectGetPropertyData(dev, &srcAddr, 0, NULL, &sz, &srcID) == noErr) {
		CFStringRef srcName = NULL;
		AudioValueTranslation tr = {&srcID, sizeof(srcID), &srcName, sizeof(srcName)};
		UInt32 tsz = sizeof(tr);
		srcAddr.mSelector = kAudioDevicePropertyDataSourceNameForIDCFString;
		if (AudioObjectGetPropertyData(dev, &srcAddr, 0, NULL, &tsz, &tr) == noErr && srcName) {
			di.source = cfCopy(srcName);
			CFRelease(srcName);
		}
	}

	return di;
}
*/
import "C"

import (
	"runtime/cgo"
	"strings"
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

// CurrentDevice reports the system default audio output device. It queries
// CoreAudio directly (cheap, thread-safe) so the caller can poll it.
func (s *Service) CurrentDevice() Device {
	di := C.currentOutputDevice()
	name := C.GoString(di.name)
	source := C.GoString(di.source)
	if di.name != nil {
		C.free(unsafe.Pointer(di.name))
	}
	if di.source != nil {
		C.free(unsafe.Pointer(di.source))
	}
	return Device{Name: name, Kind: classifyDevice(uint32(di.transport), name, source)}
}

// classifyDevice maps a CoreAudio transport type (plus name/data-source hints)
// to a DeviceKind for iconography.
func classifyDevice(transport uint32, name, source string) DeviceKind {
	ln, ls := strings.ToLower(name), strings.ToLower(source)
	has := func(hay string, kws ...string) bool {
		for _, kw := range kws {
			if strings.Contains(hay, kw) {
				return true
			}
		}
		return false
	}
	headphoneish := has(ln, "headphone", "headset", "airpod", "earbud", "buds", "beats") ||
		strings.Contains(ls, "headphone")
	// Names that read as a speaker rather than something worn on the head.
	speakerish := has(ln, "speaker", "soundbar", "homepod", "sonos", "echo", "soundlink", "boom", "flip", "charge", "monitor", "display", "tv")
	switch transport {
	case fourCC("bltn"): // built-in: speakers unless the headphone jack is live
		if strings.Contains(ls, "headphone") {
			return DeviceHeadphones
		}
		return DeviceSpeaker
	case fourCC("blue"), fourCC("blte"):
		// Most Bluetooth audio is headphones/earbuds; treat it as such unless the
		// name clearly reads as a speaker.
		if speakerish && !headphoneish {
			return DeviceBluetooth
		}
		return DeviceHeadphones
	case fourCC("usb "):
		if headphoneish {
			return DeviceHeadphones
		}
		return DeviceUSB
	case fourCC("hdmi"), fourCC("dprt"):
		return DeviceDisplay
	case fourCC("airp"):
		return DeviceAirPlay
	default:
		if headphoneish {
			return DeviceHeadphones
		}
		return DeviceSpeaker
	}
}

// fourCC packs a 4-byte CoreAudio transport code into a uint32.
func fourCC(s string) uint32 {
	return uint32(s[0])<<24 | uint32(s[1])<<16 | uint32(s[2])<<8 | uint32(s[3])
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
