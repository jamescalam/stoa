// Package station loads stoa "stations" — small YAML files, one per station,
// each either listing explicit tracks or pointing at a folder of audio files.
package station

import (
	"embed"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dhowden/tag"
	"gopkg.in/yaml.v3"

	"github.com/jamescalam/stoa/internal/config"
)

// defaultStations are the built-in stations stoa ships with. They are seeded
// into the user's stations dir on first run (see seedDefaults).
//
//go:embed defaults/*.yaml
var defaultStations embed.FS

// Track is a single playable item plus the metadata we display and credit.
type Track struct {
	File    string `yaml:"file"`
	Title   string `yaml:"title"`
	Artist  string `yaml:"artist"`
	License string `yaml:"license"`
	Source  string `yaml:"source"`
}

// Station is one entry in the picker. It is either a local station (Tracks or
// a Source folder) or a live internet-radio station (Stream URL).
type Station struct {
	Numeral     string  `yaml:"numeral"`
	Name        string  `yaml:"name"`
	Description string  `yaml:"description"`
	Shuffle     bool    `yaml:"shuffle"`
	Scene       string  `yaml:"scene"`
	Source      string  `yaml:"source"` // optional folder (bring-your-own)
	Tracks      []Track `yaml:"tracks"`
	Stream      string  `yaml:"stream"` // live radio stream URL (mutually exclusive with tracks)
}

// IsStream reports whether this station is a live internet-radio stream.
func (s Station) IsStream() bool { return s.Stream != "" }

var audioExts = map[string]bool{".mp3": true, ".wav": true, ".flac": true, ".ogg": true}

// LoadAll reads every *.yaml in the stations dir, resolves track paths against
// the audio dir, and expands folder-source stations by scanning + tag-reading.
func LoadAll() ([]Station, error) {
	dir := config.StationsDir()
	seedDefaults(dir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	audioDir := config.AudioDir()
	var stations []Station
	for _, e := range entries {
		if e.IsDir() || !isYAML(e.Name()) {
			continue
		}
		s, err := load(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		if err := s.resolve(audioDir); err != nil {
			return nil, err
		}
		stations = append(stations, s)
	}

	sort.Slice(stations, func(i, j int) bool {
		// Order by Roman-numeral value so IX sorts after VIII, not lexically
		// before V. Fall back to string order for anything unparseable.
		ri, rj := romanToInt(stations[i].Numeral), romanToInt(stations[j].Numeral)
		if ri != rj && ri != 0 && rj != 0 {
			return ri < rj
		}
		return stations[i].Numeral < stations[j].Numeral
	})
	return stations, nil
}

// romanToInt converts a Roman numeral (I, IV, XII, …) to its integer value,
// returning 0 for anything it cannot parse so callers can fall back.
func romanToInt(s string) int {
	vals := map[byte]int{'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100, 'D': 500, 'M': 1000}
	s = strings.ToUpper(strings.TrimSpace(s))
	if s == "" {
		return 0
	}
	total, prev := 0, 0
	for i := len(s) - 1; i >= 0; i-- {
		v, ok := vals[s[i]]
		if !ok {
			return 0
		}
		if v < prev {
			total -= v
		} else {
			total += v
			prev = v
		}
	}
	return total
}

// seedDefaults populates dir with the built-in stations the first time stoa
// runs. It only writes when the user has no stations yet, so their edits and
// deletions are never clobbered. Failures are non-fatal: a user who cannot
// write their config dir simply starts with an empty picker, as before.
func seedDefaults(dir string) {
	if hasYAML(dir) {
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	files, err := defaultStations.ReadDir("defaults")
	if err != nil {
		return
	}
	for _, f := range files {
		data, err := defaultStations.ReadFile(path.Join("defaults", f.Name()))
		if err != nil {
			continue
		}
		_ = os.WriteFile(filepath.Join(dir, f.Name()), data, 0o644)
	}
}

// hasYAML reports whether dir exists and holds at least one YAML file.
func hasYAML(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && isYAML(e.Name()) {
			return true
		}
	}
	return false
}

func load(path string) (Station, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Station{}, err
	}
	var s Station
	if err := yaml.Unmarshal(data, &s); err != nil {
		return Station{}, err
	}
	return s, nil
}

// resolve turns a folder source into explicit tracks and makes every track
// path absolute (relative paths are taken from audioDir; ~ is expanded).
func (s *Station) resolve(audioDir string) error {
	if s.IsStream() {
		return nil // live radio: nothing on disk to resolve
	}
	if s.Source != "" && len(s.Tracks) == 0 {
		if err := s.scanFolder(expand(s.Source)); err != nil {
			return err
		}
	}
	for i := range s.Tracks {
		f := expand(s.Tracks[i].File)
		if !filepath.IsAbs(f) {
			f = filepath.Join(audioDir, f)
		}
		s.Tracks[i].File = f
	}
	return nil
}

func (s *Station) scanFolder(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !audioExts[strings.ToLower(filepath.Ext(e.Name()))] {
			continue
		}
		full := filepath.Join(dir, e.Name())
		title, artist := readTags(full)
		if title == "" {
			title = strings.TrimSuffix(e.Name(), filepath.Ext(e.Name()))
		}
		s.Tracks = append(s.Tracks, Track{
			File:    full,
			Title:   title,
			Artist:  artist,
			License: "local", // unknown provenance — do not redistribute
		})
	}
	sort.Slice(s.Tracks, func(i, j int) bool { return s.Tracks[i].File < s.Tracks[j].File })
	return nil
}

func readTags(path string) (title, artist string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	m, err := tag.ReadFrom(f)
	if err != nil {
		return "", ""
	}
	return m.Title(), m.Artist()
}

func expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func isYAML(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}
