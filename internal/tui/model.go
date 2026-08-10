// Package tui is the Bubble Tea front-end: a station picker, a Spotify-inspired
// player bar, and a full-screen Greco-Roman visualizer canvas. While music
// plays and the keyboard is idle, the chrome (picker + player bar) auto-hides
// so the scene fills the whole terminal; any keypress brings it back.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"stoa/internal/media"
	"stoa/internal/player"
	"stoa/internal/scene"
	"stoa/internal/station"
)

var (
	ivory     = lipgloss.Color("#e8e2d0")
	dim       = lipgloss.Color("#7a7466")
	terracota = lipgloss.Color("#c96f4c")
	bronze    = lipgloss.Color("#8a9b6e")

	titleStyle    = lipgloss.NewStyle().Foreground(terracota).Bold(true)
	numeralStyle  = lipgloss.NewStyle().Foreground(bronze).Bold(true)
	nameStyle     = lipgloss.NewStyle().Foreground(ivory)
	descStyle     = lipgloss.NewStyle().Foreground(dim).Italic(true)
	selectedStyle = lipgloss.NewStyle().Foreground(terracota).Bold(true)
	helpStyle     = lipgloss.NewStyle().Foreground(dim)
	errStyle      = lipgloss.NewStyle().Foreground(terracota)

	panelStyle  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(bronze).Padding(0, 2)
	trackStyle  = lipgloss.NewStyle().Foreground(ivory).Bold(true)
	btnStyle    = lipgloss.NewStyle().Foreground(ivory)
	ppStyle     = lipgloss.NewStyle().Foreground(terracota).Bold(true)
	timeStyle   = lipgloss.NewStyle().Foreground(dim)
	progFill    = lipgloss.NewStyle().Foreground(terracota)
	progKnob    = lipgloss.NewStyle().Foreground(ivory)
	progEmpty   = lipgloss.NewStyle().Foreground(dim)
	volFull     = lipgloss.NewStyle().Foreground(terracota)
	volEmpty    = lipgloss.NewStyle().Foreground(dim)
	volCapStyle = lipgloss.NewStyle().Foreground(dim)
)

const (
	volBarHeight  = 3
	leftColWidth  = 20
	colGap        = 3
	frameInterval = 125 * time.Millisecond
)

type (
	eventMsg player.Event
	frameMsg time.Time
)

// Model is the root Bubble Tea model.
type Model struct {
	stations []station.Station
	sel      int
	player   *player.Player
	media    *media.Service
	viz      scene.Scene

	now      player.Event
	hasNow   bool
	elapsed  time.Duration
	total    time.Duration
	err      error
	volLevel int
	volMax   int
	w, h     int

	startedAt   time.Time        // animation clock origin
	animElapsed time.Duration    // wall-clock time the scene has been running
	renderMode  scene.RenderMode // ModeColorRamp (default) or ModeHalfBlock
}

// New builds the root model.
func New(stations []station.Station, p *player.Player, m *media.Service) Model {
	lvl, max := p.VolumeLevel()
	return Model{
		stations:  stations,
		player:    p,
		media:     m,
		viz:       scene.Colonnade{},
		volLevel:  lvl,
		volMax:    max,
		startedAt: time.Now(),
	}
}

// Init starts the player event listener and the animation frame loop.
func (m Model) Init() tea.Cmd { return tea.Batch(listen(m.player), frameTick()) }

func listen(p *player.Player) tea.Cmd {
	return func() tea.Msg { return eventMsg(<-p.Events()) }
}

func frameTick() tea.Cmd {
	return tea.Tick(frameInterval, func(t time.Time) tea.Msg { return frameMsg(t) })
}

// Update handles input, player events and animation frames.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil

	case frameMsg:
		m.animElapsed = time.Time(msg).Sub(m.startedAt)
		if el, ok := m.player.Progress(); ok {
			m.elapsed = el
		}
		return m, frameTick()

	case eventMsg:
		ev := player.Event(msg)
		if ev.Err != nil {
			m.err = ev.Err
		} else {
			m.now, m.hasNow, m.err = ev, true, nil
			m.total = ev.Duration
			// Read the real position: events fire on pause/resume too, so we
			// must not blindly reset elapsed to zero.
			if el, ok := m.player.Progress(); ok {
				m.elapsed = el
			} else {
				m.elapsed = 0
			}
			if m.media != nil {
				m.media.Update(media.NowPlaying{
					Title:    ev.Track.Title,
					Artist:   ev.Track.Artist,
					Duration: ev.Duration,
					Elapsed:  m.elapsed,
					Playing:  ev.Playing,
				})
			}
		}
		return m, listen(m.player)

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.player.Close()
			return m, tea.Quit
		case "up", "k":
			if m.sel > 0 {
				m.sel--
			}
		case "down", "j":
			if m.sel < len(m.stations)-1 {
				m.sel++
			}
		case "enter":
			m.play()
		case " ", "p":
			if m.hasNow {
				m.player.TogglePause()
			} else {
				m.play()
			}
		case "right", "n":
			m.player.Next()
		case "left", "b":
			m.player.Prev()
		case "-", "_":
			m.player.AdjustVolume(-1)
			m.volLevel, m.volMax = m.player.VolumeLevel()
		case "+", "=":
			m.player.AdjustVolume(1)
			m.volLevel, m.volMax = m.player.VolumeLevel()
		case "v":
			if m.renderMode == scene.ModeColorRamp {
				m.renderMode = scene.ModeHalfBlock
			} else {
				m.renderMode = scene.ModeColorRamp
			}
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) play() {
	if len(m.stations) > 0 {
		m.player.Play(m.stations[m.sel])
	}
}

// View lays out the screen: picker at the top, the visualizer filling the
// middle, and the player bar pinned full-width to the bottom. When the chrome
// is hidden the scene takes the whole screen.
func (m Model) View() string {
	w, h := m.w, m.h
	if w < 20 {
		w = 80
	}
	if h < 10 {
		h = 24
	}

	if len(m.stations) == 0 {
		return "\n  " + titleStyle.Render("S T O A") + "\n\n" +
			"  " + descStyle.Render("no stations found in ~/.config/stoa/stations") + "\n\n" +
			"  " + helpStyle.Render("q quit")
	}

	top := []string{"", "  " + titleStyle.Render("S T O A"), ""}
	for i, s := range m.stations {
		cursor := "   "
		numeral := numeralStyle.Render(pad(s.Numeral, 4))
		name := nameStyle.Render(pad(s.Name, 12))
		if i == m.sel {
			cursor = " " + selectedStyle.Render("›") + " "
			name = selectedStyle.Render(pad(s.Name, 12))
		}
		top = append(top, cursor+numeral+name+"  "+descStyle.Render(s.Description))
	}

	help := "  " + helpStyle.Render("↑/↓ station · space play/pause · ←/→ prev/next · -/+ vol · v style · q quit")
	bottom := append([]string{help}, strings.Split(m.playerBar(w), "\n")...)

	sceneRows := h - len(top) - len(bottom)
	var mid []string
	if m.viz != nil && sceneRows >= 1 {
		mid = strings.Split(m.viz.Frame(w, sceneRows, m.animElapsed, m.renderMode), "\n")
	} else {
		if sceneRows < 0 {
			sceneRows = 0
		}
		mid = make([]string, sceneRows)
	}

	lines := make([]string, 0, len(top)+len(mid)+len(bottom))
	lines = append(lines, top...)
	lines = append(lines, mid...)
	lines = append(lines, bottom...)
	return strings.Join(lines, "\n")
}

// playerBar composes the three regions into one bordered panel that spans the
// full window width.
func (m Model) playerBar(w int) string {
	contentW := w - 6 // 2 border + 2*2 padding
	if contentW < 30 {
		contentW = 30
	}

	gap := strings.Repeat(" ", colGap)
	vol := m.volumeMeter()

	var row string
	if m.err != nil {
		msgW := contentW - lipgloss.Width(vol) - colGap
		row = lipgloss.JoinHorizontal(lipgloss.Center,
			errStyle.Render(pad(truncate("! "+m.err.Error(), msgW), msgW)), gap, vol)
	} else {
		left := m.trackInfo()
		centerW := contentW - lipgloss.Width(left) - lipgloss.Width(vol) - 2*colGap
		if centerW < 20 {
			centerW = 20
		}
		row = lipgloss.JoinHorizontal(lipgloss.Center,
			left, gap, m.controlsAndProgress(centerW), gap, vol)
	}
	return panelStyle.Width(w - 2).Render(row)
}

// trackInfo is the left region: title / artist / station·position.
func (m Model) trackInfo() string {
	title, artist, sub := "—", "", "select a station"
	if m.hasNow {
		title = m.now.Track.Title
		artist = m.now.Track.Artist
		sub = fmt.Sprintf("%s · %d/%d", m.now.Station, m.now.Index, m.now.Total)
	}
	col := lipgloss.NewStyle().Width(leftColWidth)
	return col.Render(lipgloss.JoinVertical(lipgloss.Left,
		trackStyle.Render(truncate(title, leftColWidth)),
		descStyle.Render(truncate(artist, leftColWidth)),
		timeStyle.Render(truncate(sub, leftColWidth)),
	))
}

// controlsAndProgress is the center region: transport row over a seek bar.
func (m Model) controlsAndProgress(width int) string {
	pp := "▶"
	if m.hasNow && m.now.Playing {
		pp = "⏸"
	}
	controls := btnStyle.Render("⏮") + "   " + ppStyle.Render(pp) + "   " + btnStyle.Render("⏭")
	controlsRow := lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(controls)

	el := fmtDur(m.elapsed)
	tot := fmtDur(m.total)
	barW := width - lipgloss.Width(el) - lipgloss.Width(tot) - 2
	if barW < 6 {
		barW = 6
	}
	frac := 0.0
	if m.total > 0 {
		frac = float64(m.elapsed) / float64(m.total)
	}
	progRow := timeStyle.Render(el) + " " + progressBar(barW, frac) + " " + timeStyle.Render(tot)

	return lipgloss.JoinVertical(lipgloss.Center, controlsRow, progRow)
}

// volumeMeter is the right region: a bottom-filled vertical bar plus readout.
func (m Model) volumeMeter() string {
	max := m.volMax
	if max <= 0 {
		max = 1
	}
	filled := (m.volLevel*volBarHeight + max/2) / max
	rows := make([]string, 0, volBarHeight)
	for i := volBarHeight - 1; i >= 0; i-- {
		if i < filled {
			rows = append(rows, volFull.Render("┃"))
		} else {
			rows = append(rows, volEmpty.Render("╎"))
		}
	}
	bar := lipgloss.JoinVertical(lipgloss.Center, rows...)
	return lipgloss.JoinVertical(lipgloss.Center, bar, volCapStyle.Render(fmt.Sprintf("%2d", m.volLevel)))
}

// progressBar renders a Spotify-style filled bar with a knob at the play head.
func progressBar(width int, frac float64) string {
	if width < 1 {
		width = 1
	}
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	knob := int(frac * float64(width-1))
	filled := strings.Repeat("━", knob)
	empty := strings.Repeat("─", width-knob-1)
	return progFill.Render(filled) + progKnob.Render("●") + progEmpty.Render(empty)
}

func fmtDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:max0(n, 0)])
	}
	return string(r[:n-1]) + "…"
}

func max0(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func pad(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
