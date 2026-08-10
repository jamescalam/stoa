// Package player is a small gapless audio player built on gopxl/beep. It plays
// a station's tracks in order (or shuffled), auto-advancing at end of track,
// and exposes now-playing changes on an event channel for the UI to render.
package player

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	neturl "net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gopxl/beep/v2"
	"github.com/gopxl/beep/v2/effects"
	"github.com/gopxl/beep/v2/flac"
	"github.com/gopxl/beep/v2/mp3"
	"github.com/gopxl/beep/v2/speaker"
	"github.com/gopxl/beep/v2/vorbis"
	"github.com/gopxl/beep/v2/wav"

	"github.com/jamescalam/stoa/internal/station"
)

// Event is emitted whenever the now-playing state changes.
type Event struct {
	Station  string
	Track    station.Track
	Index    int // 1-based position within the current station
	Total    int
	Duration time.Duration
	Playing  bool
	Live     bool // a live internet-radio stream (no duration/seek)
	Err      error
}

type command int

const (
	cmdTrackEnded command = iota
	cmdNext
	cmdPrev
)

// maxVol is the number of discrete volume steps; defaultVol is the initial one.
const (
	maxVol     = 10
	defaultVol = 7
)

// gainFor maps a discrete volume level to an effects.Volume exponent (Base 2).
// Level maxVol is unity gain; each step down is ~3 dB; level 0 is silent.
func gainFor(level int) (vol float64, silent bool) {
	if level <= 0 {
		return 0, true
	}
	return float64(level-maxVol) * 0.5, false
}

// Player owns the single process-wide speaker. Construct one with New.
type Player struct {
	sr beep.SampleRate

	mu          sync.Mutex
	stationName string
	tracks      []station.Track
	order       []int // playback order into tracks
	pos         int   // index into order
	shuffle     bool

	ctrl       *beep.Ctrl
	vol        *effects.Volume
	stream     beep.StreamSeekCloser
	trackSR    beep.SampleRate // native rate of the current track
	trackTotal time.Duration   // full length of the current track
	volLevel   int             // 0..maxVol
	paused     bool
	live       bool               // current source is a live stream
	streamHost string             // display host for the live stream (e.g. stream.nightride.fm)
	streamStop context.CancelFunc // cancels the current stream's HTTP request
	liveKey    string             // metadata station key for the current stream
	liveTitle  string             // current track title from stream metadata
	liveArtist string             // current track artist from stream metadata

	http   *http.Client
	cmds   chan command
	events chan Event
}

// New initializes the speaker and starts the playback manager.
func New() (*Player, error) {
	sr := beep.SampleRate(44100)
	if err := speaker.Init(sr, sr.N(time.Second/10)); err != nil {
		return nil, err
	}
	p := &Player{
		sr:       sr,
		volLevel: defaultVol,
		http:     &http.Client{}, // no timeout: streams are open-ended
		cmds:     make(chan command, 8),
		events:   make(chan Event, 8),
	}
	go p.run()
	return p, nil
}

// Events returns the channel of now-playing updates.
func (p *Player) Events() <-chan Event { return p.events }

// Play starts a station from the top of its (possibly shuffled) order.
func (p *Player) Play(s station.Station) {
	p.stopStream() // leave any live stream first
	p.mu.Lock()
	p.stationName = s.Name
	p.tracks = s.Tracks
	p.shuffle = s.Shuffle
	p.order = p.buildOrder(len(s.Tracks))
	p.pos = 0
	p.mu.Unlock()

	if len(s.Tracks) == 0 {
		p.emitErr(fmt.Errorf("station %q has no tracks", s.Name))
		return
	}
	p.playAt(p.order[0])
}

// PlayStream tunes into a live internet-radio stream (an endless MP3 stream).
func (p *Player) PlayStream(name, url string) {
	p.stopStream()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		cancel()
		p.emitErr(err)
		return
	}
	req.Header.Set("User-Agent", "stoa")
	host := url
	if u, e := neturl.Parse(url); e == nil && u.Host != "" {
		host = u.Host
	}
	sseURL, metaKey, hasMeta := metadataSourceFor(url)
	// Deliberately no "Icy-MetaData" header: we want a clean MP3 body, not one
	// with interleaved metadata that would corrupt decoding.
	resp, err := p.http.Do(req)
	if err != nil {
		cancel()
		p.emitErr(fmt.Errorf("%s: %w", name, err))
		return
	}
	if resp.StatusCode != http.StatusOK {
		cancel()
		resp.Body.Close()
		p.emitErr(fmt.Errorf("%s: %s", name, resp.Status))
		return
	}

	// Buffer the network read to absorb jitter, and keep Close wired to the body.
	rc := &bufReadCloser{Reader: bufio.NewReaderSize(resp.Body, 256<<10), closer: resp.Body}
	streamer, format, err := mp3.Decode(rc)
	if err != nil {
		cancel()
		resp.Body.Close()
		p.emitErr(fmt.Errorf("%s: %w", name, err))
		return
	}

	p.mu.Lock()
	g, silent := gainFor(p.volLevel)
	p.mu.Unlock()

	var s beep.Streamer = streamer
	if format.SampleRate != p.sr {
		s = beep.Resample(4, format.SampleRate, p.sr, streamer)
	}
	ctrl := &beep.Ctrl{Streamer: s}
	vol := &effects.Volume{Streamer: ctrl, Base: 2, Volume: g, Silent: silent}

	speaker.Clear()

	p.mu.Lock()
	if p.stream != nil {
		p.stream.Close()
	}
	p.ctrl, p.vol, p.stream = ctrl, vol, streamer
	p.trackSR = format.SampleRate
	p.trackTotal = 0
	p.paused = false
	p.stationName = name
	p.streamHost = host
	p.liveKey, p.liveTitle, p.liveArtist = metaKey, "", ""
	p.tracks, p.order, p.pos = nil, nil, 0 // no track list; next/prev become no-ops
	p.live = true
	p.streamStop = cancel
	p.mu.Unlock()

	speaker.Play(vol) // endless: no Seq/Callback
	p.emitLive()

	if hasMeta {
		go p.pollMetadata(ctx, sseURL, metaKey)
	}
}

// metadataSourceFor returns the SSE metadata endpoint and station key for a
// stream URL, if one is known. Currently only Nightride FM is supported.
func metadataSourceFor(streamURL string) (sseURL, key string, ok bool) {
	u, err := neturl.Parse(streamURL)
	if err != nil {
		return "", "", false
	}
	if strings.Contains(u.Host, "nightride.fm") {
		return "https://nightride.fm/meta", strings.TrimSuffix(path.Base(u.Path), ".mp3"), true
	}
	return "", "", false
}

// pollMetadata keeps an SSE connection to the metadata endpoint open, updating
// now-playing whenever the current station's track changes. It reconnects until
// the stream's context is cancelled.
func (p *Player) pollMetadata(ctx context.Context, sseURL, key string) {
	for ctx.Err() == nil {
		p.readMeta(ctx, sseURL, key)
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second): // brief backoff, then reconnect
		}
	}
}

func (p *Player) readMeta(ctx context.Context, sseURL, key string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sseURL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "stoa")
	resp, err := p.http.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	var data strings.Builder
	for sc.Scan() {
		line := sc.Text()
		if line == "" { // blank line terminates an SSE event
			if data.Len() > 0 {
				p.handleMeta(data.String(), key)
				data.Reset()
			}
			continue
		}
		if v, ok := strings.CutPrefix(line, "data:"); ok {
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(v))
		}
	}
}

func (p *Player) handleMeta(data, key string) {
	if data == "keepalive" {
		return
	}
	var items []struct {
		Station string `json:"station"`
		Title   string `json:"title"`
		Artist  string `json:"artist"`
	}
	if err := json.Unmarshal([]byte(data), &items); err != nil || len(items) == 0 {
		return
	}
	it := items[0]
	if it.Station != key {
		return
	}
	p.mu.Lock()
	// Ignore if we've since switched away from this stream.
	if !p.live || p.liveKey != key {
		p.mu.Unlock()
		return
	}
	changed := it.Title != p.liveTitle || it.Artist != p.liveArtist
	p.liveTitle, p.liveArtist = it.Title, it.Artist
	p.mu.Unlock()
	if changed {
		p.emitLive()
	}
}

// stopStream cancels any active live stream's HTTP request.
func (p *Player) stopStream() {
	p.mu.Lock()
	cancel := p.streamStop
	p.streamStop = nil
	p.live = false
	p.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// bufReadCloser adds buffering to a stream while closing the underlying body.
type bufReadCloser struct {
	*bufio.Reader
	closer io.Closer
}

func (b *bufReadCloser) Close() error { return b.closer.Close() }

// Next skips to the following track.
func (p *Player) Next() {
	select {
	case p.cmds <- cmdNext:
	default:
	}
}

// Prev skips to the previous track.
func (p *Player) Prev() {
	select {
	case p.cmds <- cmdPrev:
	default:
	}
}

// TogglePause pauses or resumes the current track.
func (p *Player) TogglePause() {
	p.mu.Lock()
	if p.ctrl == nil {
		p.mu.Unlock()
		return
	}
	speaker.Lock()
	p.ctrl.Paused = !p.ctrl.Paused
	p.paused = p.ctrl.Paused
	speaker.Unlock()
	p.mu.Unlock()
	p.emitCurrent()
}

// SetPaused forces the paused state (used by OS media commands where play and
// pause are distinct). A no-op if nothing is loaded.
func (p *Player) SetPaused(paused bool) {
	p.mu.Lock()
	if p.ctrl == nil {
		p.mu.Unlock()
		return
	}
	speaker.Lock()
	p.ctrl.Paused = paused
	p.paused = paused
	speaker.Unlock()
	p.mu.Unlock()
	p.emitCurrent()
}

// AdjustVolume changes the volume by delta steps, clamped to [0, maxVol].
func (p *Player) AdjustVolume(delta int) {
	p.mu.Lock()
	p.volLevel += delta
	if p.volLevel > maxVol {
		p.volLevel = maxVol
	}
	if p.volLevel < 0 {
		p.volLevel = 0
	}
	g, silent := gainFor(p.volLevel)
	if p.vol != nil {
		speaker.Lock()
		p.vol.Volume = g
		p.vol.Silent = silent
		speaker.Unlock()
	}
	p.mu.Unlock()
}

// VolumeLevel returns the current level and the maximum.
func (p *Player) VolumeLevel() (level, max int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.volLevel, maxVol
}

// Progress returns how far into the current track playback has reached.
// ok is false when nothing is loaded.
func (p *Player) Progress() (elapsed time.Duration, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stream == nil || p.trackSR == 0 {
		return 0, false
	}
	speaker.Lock()
	pos := p.stream.Position()
	speaker.Unlock()
	return p.trackSR.D(pos), true
}

// Close stops playback and releases the speaker.
func (p *Player) Close() {
	p.stopStream()
	speaker.Clear()
	p.mu.Lock()
	if p.stream != nil {
		p.stream.Close()
		p.stream = nil
	}
	p.mu.Unlock()
}

func (p *Player) run() {
	for c := range p.cmds {
		p.mu.Lock()
		if len(p.order) == 0 {
			p.mu.Unlock()
			continue
		}
		if c == cmdPrev {
			p.pos--
		} else {
			// cmdNext and cmdTrackEnded both advance forward.
			p.pos++
		}
		switch {
		case p.pos < 0:
			p.pos = len(p.order) - 1
		case p.pos >= len(p.order):
			p.pos = 0
			if p.shuffle {
				p.order = p.buildOrder(len(p.tracks))
			}
		}
		idx := p.order[p.pos]
		p.mu.Unlock()
		p.playAt(idx)
	}
}

func (p *Player) playAt(idx int) {
	p.mu.Lock()
	if idx < 0 || idx >= len(p.tracks) {
		p.mu.Unlock()
		return
	}
	t := p.tracks[idx]
	p.mu.Unlock()

	streamer, format, err := decode(t.File)
	if err != nil {
		p.emitErr(fmt.Errorf("%s: %w", filepath.Base(t.File), err))
		return
	}

	var s beep.Streamer = streamer
	if format.SampleRate != p.sr {
		s = beep.Resample(4, format.SampleRate, p.sr, streamer)
	}
	p.mu.Lock()
	g, silent := gainFor(p.volLevel)
	p.mu.Unlock()

	ctrl := &beep.Ctrl{Streamer: s}
	vol := &effects.Volume{Streamer: ctrl, Base: 2, Volume: g, Silent: silent}

	speaker.Clear()

	p.mu.Lock()
	if p.stream != nil {
		p.stream.Close()
	}
	p.ctrl, p.vol, p.stream = ctrl, vol, streamer
	p.trackSR = format.SampleRate
	p.trackTotal = format.SampleRate.D(streamer.Len())
	p.paused = false
	p.pos = indexInOrder(p.order, idx, p.pos)
	p.mu.Unlock()

	speaker.Play(beep.Seq(vol, beep.Callback(func() {
		select {
		case p.cmds <- cmdTrackEnded:
		default:
		}
	})))

	p.emit()
}

func (p *Player) buildOrder(n int) []int {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	if p.shuffle {
		rand.Shuffle(n, func(i, j int) { order[i], order[j] = order[j], order[i] })
	}
	return order
}

func (p *Player) emit() {
	p.mu.Lock()
	if len(p.order) == 0 {
		p.mu.Unlock()
		return
	}
	idx := p.order[p.pos]
	ev := Event{
		Station:  p.stationName,
		Track:    p.tracks[idx],
		Index:    p.pos + 1,
		Total:    len(p.tracks),
		Duration: p.trackTotal,
		Playing:  !p.paused,
	}
	p.mu.Unlock()
	p.send(ev)
}

// emitCurrent emits the right event shape for the current source.
func (p *Player) emitCurrent() {
	p.mu.Lock()
	live := p.live
	p.mu.Unlock()
	if live {
		p.emitLive()
	} else {
		p.emit()
	}
}

// emitLive emits now-playing state for a live stream. It shows the current
// track from stream metadata once known, falling back to the station name.
func (p *Player) emitLive() {
	p.mu.Lock()
	title, artist := p.stationName, p.streamHost
	if p.liveTitle != "" {
		title, artist = p.liveTitle, p.liveArtist
	}
	ev := Event{
		Station: p.stationName,
		Track:   station.Track{Title: title, Artist: artist},
		Live:    true,
		Playing: !p.paused,
	}
	p.mu.Unlock()
	p.send(ev)
}

func (p *Player) emitErr(err error) { p.send(Event{Err: err}) }

func (p *Player) send(ev Event) {
	select {
	case p.events <- ev:
	default:
	}
}

func decode(path string) (beep.StreamSeekCloser, beep.Format, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, beep.Format{}, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp3":
		return mp3.Decode(f)
	case ".wav":
		return wav.Decode(f)
	case ".flac":
		return flac.Decode(f)
	case ".ogg":
		return vorbis.Decode(f)
	default:
		f.Close()
		return nil, beep.Format{}, fmt.Errorf("unsupported format %q", filepath.Ext(path))
	}
}

// indexInOrder returns the position of idx within order, preferring the current
// guess so shuffled orders with the (rare) duplicate index stay put.
func indexInOrder(order []int, idx, guess int) int {
	if guess >= 0 && guess < len(order) && order[guess] == idx {
		return guess
	}
	for i, v := range order {
		if v == idx {
			return i
		}
	}
	return guess
}
