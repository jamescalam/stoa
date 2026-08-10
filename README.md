# stoa

A minimal in-terminal music radio with a slow, Greco-Roman visualizer — built
for focus. Pick a "station", and watch a quiet colonnade drift under an arcing
sun rendered in colored ASCII, while a Spotify-style bar handles playback.

> macOS only for now (it integrates with Control Center, the media keys, and
> AirPods). A truecolor terminal is recommended.

## Install

```sh
brew install jamescalam/stoa/stoa
```

Or build from source (requires Go 1.25+ and Xcode command-line tools for cgo):

```sh
git clone https://github.com/jamescalam/stoa
cd stoa
go build -o stoa .
```

## Usage

```sh
stoa
```

| Key        | Action              |
| ---------- | ------------------- |
| `↑` / `↓`  | select station      |
| `space`    | play / pause        |
| `←` / `→`  | previous / next     |
| `-` / `+`  | volume              |
| `v`        | toggle render style |
| `q`        | quit                |

Playback also responds to the macOS media keys, Control Center, and AirPods.

## Stations

Stations are YAML files in `~/.config/stoa/stations/`. Each points at explicit
tracks or a folder of your own audio:

```yaml
numeral: I
name: OUTRUN
description: no-vocal synthwave for night driving
shuffle: true
scene: colonnade

# either a folder of your own files...
source: ~/Music/synthwave

# ...or explicit tracks (paths relative to ~/.local/share/stoa/audio)
tracks:
  - file: outrun/track.mp3
    title: Track Title
    artist: Artist
    license: CC-BY-4.0
    source: https://example.com
```

Audio lives in `~/.local/share/stoa/audio/`. Supported formats: MP3, FLAC, WAV,
OGG.

## License

MIT
