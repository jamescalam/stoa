package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jamescalam/stoa/internal/player"
	"github.com/jamescalam/stoa/internal/scene"
	"github.com/jamescalam/stoa/internal/station"
)

// TestLayoutIntegration verifies View is exactly h lines in both states, that
// an active station swaps the picker for the scene (its identity moving to the
// player bar), and that the idle picker lists stations. The logo box stays
// pinned bottom-right throughout.
func TestLayoutIntegration(t *testing.T) {
	base := Model{
		stations: []station.Station{{Numeral: "I", Name: "OUTRUN", Description: "x"}},
		viz:      scene.Colonnade{},
		volLevel: 7, volMax: 10, w: 80, h: 24,
		animElapsed: 60 * time.Second,
		st:          newStyles(themes[0].pal),
	}

	// Idle: the picker is on screen, the scene is not.
	idle := base.View()
	if got := len(strings.Split(idle, "\n")); got != 24 {
		t.Fatalf("idle: got %d lines, want 24", got)
	}
	if !strings.Contains(idle, "OUTRUN") {
		t.Error("idle: expected the station picker on screen")
	}
	if !strings.Contains(idle, "s t o a") {
		t.Error("idle: expected the logo masthead on screen")
	}

	// Active: the scene fills the body, the picker list is gone, and the
	// station identity now lives in the player bar.
	m := base
	m.active = true
	m.curNum, m.curName = "I", "OUTRUN"
	v := m.View()
	if got := len(strings.Split(v, "\n")); got != 24 {
		t.Fatalf("active: got %d lines, want 24", got)
	}
	if !strings.Contains(v, "\x1b[38;2") {
		t.Error("active: expected coloured scene cells in the body")
	}
	if !strings.Contains(v, "╰") {
		t.Error("active: expected the player bar border on screen")
	}
	if !strings.Contains(v, "OUTRUN") {
		t.Error("active: expected the station identity in the player bar")
	}
}

// TestRenderStates is a visual harness: it prints the UI in several states so
// layout can be eyeballed without an interactive terminal. It asserts nothing.
func TestRenderStates(t *testing.T) {
	stations := []station.Station{
		{Numeral: "I", Name: "OUTRUN", Description: "no-vocal synthwave for night driving"},
		{Numeral: "II", Name: "NOCTURNE", Description: "slow ambient for deep focus"},
	}
	base := Model{stations: stations, viz: scene.Colonnade{}, volLevel: 7, volMax: 10, w: 78, h: 20, st: newStyles(themes[0].pal)}

	fmt.Println("\n===== IDLE (picker, no scene) =====")
	fmt.Println(base.View())

	playing := base
	playing.active = true
	playing.curNum, playing.curName = "I", "OUTRUN"
	playing.hasNow = true
	playing.now = player.Event{
		Station: "OUTRUN",
		Track:   station.Track{Title: "Motion Blur", Artist: "Nihilore"},
		Index:   2, Total: 5, Playing: true, Duration: 3*time.Minute + 57*time.Second,
	}
	playing.total = playing.now.Duration
	playing.elapsed = 1*time.Minute + 23*time.Second
	fmt.Println("\n===== PLAYING (1:23 / 3:57, vol 7) =====")
	fmt.Println(playing.View())

	paused := playing
	paused.now.Playing = false
	paused.volLevel = 3
	paused.elapsed = 12 * time.Second
	fmt.Println("\n===== PAUSED (0:12 / 3:57, vol 3) =====")
	fmt.Println(paused.View())

	longName := playing
	longName.now.Track = station.Track{Title: "At the Time of Encounter, Two Hands Joining", Artist: "Nihilore"}
	longName.volLevel = 10
	longName.elapsed = 3*time.Minute + 40*time.Second
	fmt.Println("\n===== LONG TITLE (near end, vol 10) =====")
	fmt.Println(longName.View())
}
