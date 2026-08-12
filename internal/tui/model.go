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
	"github.com/charmbracelet/x/ansi"

	"github.com/jamescalam/stoa/internal/media"
	"github.com/jamescalam/stoa/internal/player"
	"github.com/jamescalam/stoa/internal/scene"
	"github.com/jamescalam/stoa/internal/station"
)

// palette is the small set of colour roles every chrome style is derived from.
// It drives the interface only — the visualizer paints its own RGB and is never
// touched by the active theme.
type palette struct {
	text      lipgloss.Color   // primary readable text (titles, names)
	dim       lipgloss.Color   // muted text (descriptions, help, empties)
	accent    lipgloss.Color   // headline accent (title, transport, progress, live)
	secondary lipgloss.Color   // numerals and panel borders
	gradient  []lipgloss.Color // logo wordmark ramp, low→high
}

// theme is a named palette shown in the settings menu.
type theme struct {
	name string
	pal  palette
}

// themes lists the selectable interface themes. The first is the default.
var themes = []theme{
	{
		name: "Vector Protocol", // cyberpunk: deep purple dim, neon green/cyan
		pal: palette{
			text:      lipgloss.Color("#EEFFFF"),
			dim:       lipgloss.Color("#6766b3"),
			accent:    lipgloss.Color("#00FF9C"),
			secondary: lipgloss.Color("#00b0ff"),
			gradient: []lipgloss.Color{
				lipgloss.Color("#00FF9C"),
				lipgloss.Color("#00ffc8"),
				lipgloss.Color("#00b0ff"),
				lipgloss.Color("#6095ff"),
				lipgloss.Color("#EEFFFF"),
			},
		},
	},
	{
		name: "Greco-Roman", // warm ivory, terracota and olive-bronze
		pal: palette{
			text:      lipgloss.Color("#e8e2d0"),
			dim:       lipgloss.Color("#7a7466"),
			accent:    lipgloss.Color("#c96f4c"),
			secondary: lipgloss.Color("#8a9b6e"),
			gradient: []lipgloss.Color{
				lipgloss.Color("#8a9b6e"),
				lipgloss.Color("#b0824f"),
				lipgloss.Color("#c96f4c"),
				lipgloss.Color("#d98b63"),
				lipgloss.Color("#e8e2d0"),
			},
		},
	},
}

// styleSet holds every lipgloss style the interface uses, derived from one
// palette so switching themes is a single rebuild.
type styleSet struct {
	title, numeral, name, desc, selected, help, err        lipgloss.Style
	panel, track, btn, pp, timeS                           lipgloss.Style
	progFill, progKnob, progEmpty, live, volFull, volEmpty lipgloss.Style
	volCap, logo, setHead, setSel, setDim                  lipgloss.Style
	gradient                                               []lipgloss.Color
}

// newStyles builds a styleSet from a palette.
func newStyles(p palette) styleSet {
	rounded := func(border lipgloss.Color, padX int) lipgloss.Style {
		return lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(border).Padding(0, padX)
	}
	return styleSet{
		title:     lipgloss.NewStyle().Foreground(p.accent).Bold(true),
		numeral:   lipgloss.NewStyle().Foreground(p.secondary).Bold(true),
		name:      lipgloss.NewStyle().Foreground(p.text),
		desc:      lipgloss.NewStyle().Foreground(p.dim).Italic(true),
		selected:  lipgloss.NewStyle().Foreground(p.accent).Bold(true),
		help:      lipgloss.NewStyle().Foreground(p.dim),
		err:       lipgloss.NewStyle().Foreground(p.accent),
		panel:     rounded(p.secondary, 2),
		track:     lipgloss.NewStyle().Foreground(p.text).Bold(true),
		btn:       lipgloss.NewStyle().Foreground(p.text),
		pp:        lipgloss.NewStyle().Foreground(p.accent).Bold(true),
		timeS:     lipgloss.NewStyle().Foreground(p.dim),
		progFill:  lipgloss.NewStyle().Foreground(p.accent),
		progKnob:  lipgloss.NewStyle().Foreground(p.text),
		progEmpty: lipgloss.NewStyle().Foreground(p.dim),
		live:      lipgloss.NewStyle().Foreground(p.accent).Bold(true),
		volFull:   lipgloss.NewStyle().Foreground(p.accent),
		volEmpty:  lipgloss.NewStyle().Foreground(p.dim),
		volCap:    lipgloss.NewStyle().Foreground(p.dim),
		// The logo box wears the accent border, in the spirit of charmbracelet's
		// crush and our own slackterm wordmark.
		logo:     rounded(p.accent, 1),
		setHead:  lipgloss.NewStyle().Foreground(p.accent).Bold(true),
		setSel:   lipgloss.NewStyle().Foreground(p.accent).Bold(true),
		setDim:   lipgloss.NewStyle().Foreground(p.dim),
		gradient: p.gradient,
	}
}

const (
	volBarHeight = 3
	// barContentH is the height of the player bar's content (the tallest region,
	// the 4-line volume meter and track info). The inline logo matches it so the
	// two boxes share a bottom edge.
	barContentH   = volBarHeight + 1
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
	active   bool   // a station has been selected: hide the picker, show the scene
	curNum   string // numeral of the selected station (for the player bar)
	curName  string // name of the selected station (for the player bar)
	elapsed  time.Duration
	total    time.Duration
	err      error
	volLevel int
	volMax   int
	w, h     int

	startedAt   time.Time        // animation clock origin
	animElapsed time.Duration    // wall-clock time the scene has been running
	renderMode  scene.RenderMode // ModeColorRamp (default) or ModeHalfBlock

	settings bool     // settings menu open (ctrl+s)
	themeIdx int      // index into themes; drives st
	setSel   int      // cursor within the settings theme list
	st       styleSet // styles for the active theme
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
		st:        newStyles(themes[0].pal), // default theme: Vector Protocol
	}
}

// applyTheme switches to themes[i] and rebuilds the interface styles.
func (m *Model) applyTheme(i int) {
	if i < 0 || i >= len(themes) {
		return
	}
	m.themeIdx = i
	m.st = newStyles(themes[i].pal)
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
		// Quit and the settings toggle work from any view.
		switch msg.String() {
		case "ctrl+c":
			m.player.Close()
			return m, tea.Quit
		case "ctrl+s":
			m.settings = !m.settings
			if m.settings {
				m.setSel = m.themeIdx
			}
			return m, nil
		}

		// While the settings menu is open it owns navigation.
		if m.settings {
			switch msg.String() {
			case "esc", "enter", "q":
				m.settings = false
			case "up", "k":
				if m.setSel > 0 {
					m.setSel--
					m.applyTheme(m.setSel) // live preview
				}
			case "down", "j":
				if m.setSel < len(themes)-1 {
					m.setSel++
					m.applyTheme(m.setSel)
				}
			}
			return m, nil
		}

		switch msg.String() {
		case "q":
			m.player.Close()
			return m, tea.Quit
		case "esc":
			// Back to the station picker without interrupting playback: the
			// player bar and logo stay pinned at the bottom.
			m.active = false
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
	if len(m.stations) == 0 {
		return
	}
	s := m.stations[m.sel]
	m.active = true
	m.curNum, m.curName = s.Numeral, s.Name
	if s.IsStream() {
		m.player.PlayStream(s.Name, s.Stream)
	} else {
		m.player.Play(s)
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
		return "\n  " + m.st.title.Render("R A D I O S") + "\n\n" +
			"  " + m.st.desc.Render("no stations found in ~/.config/stoa/stations") + "\n\n" +
			"  " + m.st.help.Render("q quit")
	}

	help := "  " + m.st.help.Render(m.helpText())

	// The logo sits inline to the right of the player bar, sharing its height.
	logo := m.logoView(barContentH)
	bar := m.playerBar(w - lipgloss.Width(logo))
	barRow := lipgloss.JoinHorizontal(lipgloss.Top, bar, logo)
	bottom := append([]string{help}, strings.Split(barRow, "\n")...)

	bodyRows := h - len(bottom)
	if bodyRows < 0 {
		bodyRows = 0
	}

	var body []string
	switch {
	case m.settings:
		body = m.settingsView(bodyRows)
	case m.active:
		// A station is selected: the visualizer takes over the whole body; the
		// picker is gone (its identity now lives in the player bar).
		if m.viz != nil && bodyRows >= 1 {
			body = strings.Split(m.viz.Frame(w, bodyRows, m.animElapsed, m.renderMode), "\n")
		} else {
			body = make([]string, bodyRows)
		}
	default:
		body = m.pickerView(bodyRows)
	}

	lines := make([]string, 0, len(body)+len(bottom))
	lines = append(lines, body...)
	lines = append(lines, bottom...)
	return strings.Join(lines, "\n")
}

// helpText is the keybinding hint under the body, adapted to the current view.
func (m Model) helpText() string {
	if m.settings {
		return "↑/↓ theme · enter/esc close · q quit"
	}
	return "↑/↓ station · space play/pause · ←/→ prev/next · -/+ vol · ctrl+s settings · esc stations · v style · q quit"
}

// pickerView is the idle body: the S T O A masthead over the station list.
func (m Model) pickerView(rows int) []string {
	body := make([]string, 0, rows)
	body = append(body, "", "  "+m.st.title.Render("S T O A"), "")
	for i, s := range m.stations {
		cursor := "   "
		numeral := m.st.numeral.Render(pad(s.Numeral, 4))
		name := m.st.name.Render(pad(s.Name, 12))
		if i == m.sel {
			cursor = " " + m.st.selected.Render("›") + " "
			name = m.st.selected.Render(pad(s.Name, 12))
		}
		body = append(body, cursor+numeral+name+"  "+m.st.desc.Render(s.Description))
	}
	for len(body) < rows {
		body = append(body, "")
	}
	return body
}

// settingsView is the settings body: currently a single theme picker. Themes
// restyle the interface only; the visualizer keeps its own colours.
func (m Model) settingsView(rows int) []string {
	body := make([]string, 0, rows)
	body = append(body, "", "  "+m.st.setHead.Render("C O N F I G"), "")
	body = append(body, "  "+m.st.setDim.Render("Theme"), "")
	for i, t := range themes {
		cursor := "   "
		label := m.st.name.Render(t.name)
		if i == m.setSel {
			cursor = " " + m.st.setSel.Render("›") + " "
			label = m.st.setSel.Render(t.name)
		}
		mark := ""
		if i == m.themeIdx {
			mark = "  " + m.st.setDim.Render("(active)")
		}
		body = append(body, cursor+label+mark)
	}
	body = append(body, "", "  "+m.st.setDim.Render("themes restyle the interface, not the colonnade"))
	for len(body) < rows {
		body = append(body, "")
	}
	if len(body) > rows {
		body = body[:rows]
	}
	return body
}

// logoView renders the branded masthead: a gradient wordmark over a rule,
// inside a rounded accent box whose content is centred in a contentH-tall cell
// so the box lines up with the player bar beside it.
func (m Model) logoView(contentH int) string {
	art := lipgloss.JoinVertical(lipgloss.Center,
		m.gradientRunes("▞▚ s t o a"),
		m.gradientRunes(strings.Repeat("─", 10)),
	)
	return m.st.logo.Height(contentH).AlignVertical(lipgloss.Center).Render(art)
}

// gradientRunes colours each rune of s along the active theme's logo gradient
// (bold), so the wordmark fades across its width.
func (m Model) gradientRunes(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return s
	}
	grad := m.st.gradient
	if len(grad) == 0 {
		return s
	}
	var b strings.Builder
	for i, r := range runes {
		c := grad[i*len(grad)/len(runes)]
		b.WriteString(lipgloss.NewStyle().Foreground(c).Bold(true).Render(string(r)))
	}
	return b.String()
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
			m.st.err.Render(pad(truncate("! "+m.err.Error(), msgW), msgW)), gap, vol)
	} else {
		left := m.trackInfo()
		centerW := contentW - lipgloss.Width(left) - lipgloss.Width(vol) - 2*colGap
		if centerW < 20 {
			centerW = 20
		}
		row = lipgloss.JoinHorizontal(lipgloss.Center,
			left, gap, m.controlsAndProgress(centerW), gap, vol)
	}
	return m.st.panel.Width(w - 2).Render(row)
}

// trackInfo is the left region: station identity (numeral · name) over the
// track title / artist / position. The identity line is where the picker's
// numeral and name go once a station has been selected.
func (m Model) trackInfo() string {
	ident := m.st.desc.Render("no station")
	if m.curName != "" {
		ident = m.st.numeral.Render(m.curNum) + " " + m.st.name.Render(m.curName)
	}
	title, artist, sub := "—", "", "select a station"
	if m.hasNow {
		title = m.now.Track.Title
		artist = m.now.Track.Artist
		if m.now.Live {
			sub = "live radio"
		} else {
			sub = fmt.Sprintf("%d/%d", m.now.Index, m.now.Total)
		}
	}
	col := lipgloss.NewStyle().Width(leftColWidth)
	return col.Render(lipgloss.JoinVertical(lipgloss.Left,
		truncateStyled(ident, leftColWidth),
		m.st.track.Render(truncate(title, leftColWidth)),
		m.st.desc.Render(truncate(artist, leftColWidth)),
		m.st.timeS.Render(truncate(sub, leftColWidth)),
	))
}

// truncateStyled trims an already-styled string to n cells, ANSI-aware.
func truncateStyled(s string, n int) string {
	if lipgloss.Width(s) <= n {
		return s
	}
	return ansi.Truncate(s, n-1, "") + "…"
}

// controlsAndProgress is the center region: transport row over a seek bar.
func (m Model) controlsAndProgress(width int) string {
	pp := "▶"
	if m.hasNow && m.now.Playing {
		pp = "⏸"
	}
	controls := m.st.btn.Render("⏮") + "   " + m.st.pp.Render(pp) + "   " + m.st.btn.Render("⏭")
	controlsRow := lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(controls)

	// Live streams have no duration or seek — show an on-air indicator instead.
	if m.hasNow && m.now.Live {
		line := m.st.live.Render("◉ LIVE") + m.st.timeS.Render("   "+fmtDur(m.elapsed)+" on air")
		liveRow := lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(line)
		return lipgloss.JoinVertical(lipgloss.Center, controlsRow, liveRow)
	}

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
	progRow := m.st.timeS.Render(el) + " " + m.progressBar(barW, frac) + " " + m.st.timeS.Render(tot)

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
			rows = append(rows, m.st.volFull.Render("┃"))
		} else {
			rows = append(rows, m.st.volEmpty.Render("╎"))
		}
	}
	bar := lipgloss.JoinVertical(lipgloss.Center, rows...)
	return lipgloss.JoinVertical(lipgloss.Center, bar, m.st.volCap.Render(fmt.Sprintf("%2d", m.volLevel)))
}

// progressBar renders a Spotify-style filled bar with a knob at the play head.
func (m Model) progressBar(width int, frac float64) string {
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
	return m.st.progFill.Render(filled) + m.st.progKnob.Render("●") + m.st.progEmpty.Render(empty)
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
