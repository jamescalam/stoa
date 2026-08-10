// Package config resolves stoa's on-disk locations following the XDG base
// directory spec, with the conventional fallbacks when the env vars are unset.
package config

import (
	"os"
	"path/filepath"
)

func xdg(envVar string, fallback ...string) string {
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(append([]string{home}, fallback...)...)
}

// ConfigDir is where stations/*.yaml and config.yaml live (small, backed up).
func ConfigDir() string { return filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), "stoa") }

// DataDir holds curated audio (large, backed up).
func DataDir() string { return filepath.Join(xdg("XDG_DATA_HOME", ".local", "share"), "stoa") }

// CacheDir holds regenerable artifacts (artwork, fft) — safe to delete.
func CacheDir() string { return filepath.Join(xdg("XDG_CACHE_HOME", ".cache"), "stoa") }

// StationsDir is the per-station YAML directory. Dropping a file here adds a station.
func StationsDir() string { return filepath.Join(ConfigDir(), "stations") }

// AudioDir is the root that station track paths are resolved against.
func AudioDir() string { return filepath.Join(DataDir(), "audio") }
