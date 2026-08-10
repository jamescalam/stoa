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

// TestLayoutIntegration verifies the scene fills the middle while the picker
// and player bar stay on screen, and that View is exactly h lines.
func TestLayoutIntegration(t *testing.T) {
	m := Model{
		stations: []station.Station{{Numeral: "I", Name: "OUTRUN", Description: "x"}},
		viz:      scene.Colonnade{},
		volLevel: 7, volMax: 10, w: 80, h: 24,
		animElapsed: 60 * time.Second,
	}

	v := m.View()
	if got := len(strings.Split(v, "\n")); got != 24 {
		t.Fatalf("got %d lines, want 24", got)
	}
	if !strings.Contains(v, "\x1b[38;2") {
		t.Error("expected coloured scene cells in the middle")
	}
	if !strings.Contains(v, "╰") {
		t.Error("expected the player bar border to stay on screen")
	}
	if !strings.Contains(v, "OUTRUN") {
		t.Error("expected the station picker to stay on screen")
	}
}

// TestRenderStates is a visual harness: it prints the UI in several states so
// layout can be eyeballed without an interactive terminal. It asserts nothing.
func TestRenderStates(t *testing.T) {
	stations := []station.Station{
		{Numeral: "I", Name: "OUTRUN", Description: "no-vocal synthwave for night driving"},
		{Numeral: "II", Name: "NOCTURNE", Description: "slow ambient for deep focus"},
	}
	base := Model{stations: stations, volLevel: 7, volMax: 10, w: 78, h: 20}

	fmt.Println("\n===== IDLE =====")
	fmt.Println(base.View())

	playing := base
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
