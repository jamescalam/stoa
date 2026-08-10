// Command stoa is a minimal in-terminal music radio with a Greco-Roman
// visualizer aesthetic. This entrypoint loads stations, starts the audio
// player, wires up OS media controls, and hands off to the Bubble Tea UI.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jamescalam/stoa/internal/media"
	"github.com/jamescalam/stoa/internal/player"
	"github.com/jamescalam/stoa/internal/station"
	"github.com/jamescalam/stoa/internal/tui"
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	for _, a := range os.Args[1:] {
		switch a {
		case "--version", "-v", "version":
			fmt.Println("stoa", version)
			return
		case "--help", "-h", "help":
			fmt.Println("stoa — minimal in-terminal music radio with a Greco-Roman visualizer")
			fmt.Println("\nStations live in ~/.config/stoa/stations/*.yaml; run with no arguments to launch.")
			fmt.Println("\nKeys: ↑/↓ station · space play/pause · ←/→ prev/next · -/+ vol · v style · q quit")
			return
		}
	}

	stations, err := station.LoadAll()
	if err != nil {
		fmt.Fprintln(os.Stderr, "stoa: loading stations:", err)
		os.Exit(1)
	}

	p, err := player.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "stoa: audio init:", err)
		os.Exit(1)
	}
	defer p.Close()

	// OS media controls (Control Center, media keys, AirPods on macOS).
	svc := media.New(media.Commands{
		PlayPause: p.TogglePause,
		Play:      func() { p.SetPaused(false) },
		Pause:     func() { p.SetPaused(true) },
		Next:      p.Next,
		Prev:      p.Prev,
		Stop:      func() { p.SetPaused(true) },
	})
	defer svc.Close()

	prog := tea.NewProgram(tui.New(stations, p, svc), tea.WithAltScreen())

	// On macOS svc.Run pumps the Cocoa run loop on the main thread and runs the
	// UI on a background goroutine; elsewhere it just runs the UI inline.
	err = svc.Run(func() error {
		_, err := prog.Run()
		return err
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "stoa:", err)
		os.Exit(1)
	}
}
