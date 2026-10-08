package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "model-citizen.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadMissingFileUsesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg != Default() {
		t.Fatalf("expected defaults, got %+v", cfg)
	}
}

func TestLoadPartialFileMergesOverDefaults(t *testing.T) {
	path := writeConfig(t, `
[music]
volume_db = -20.0
search_timeout = "30s"

[music.cache]
max_tracks = 10
`)

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	want := Default()
	want.Music.VolumeDB = -20
	want.Music.SearchTimeout = 30 * time.Second
	want.Music.Cache.MaxTracks = 10
	if cfg != want {
		t.Fatalf("expected %+v, got %+v", want, cfg)
	}
}

func TestLoadUnknownKeyErrors(t *testing.T) {
	path := writeConfig(t, `
[music]
volum_db = -20.0
`)

	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), "music.volum_db") {
		t.Fatalf("expected unknown key error naming music.volum_db, got %v", err)
	}
}

func TestLoadInvalidValuesError(t *testing.T) {
	tests := map[string]string{
		"bad duration":  "[voice]\njoin_timeout = \"soon\"",
		"zero capacity": "[music]\nqueue_capacity = 0",
		"bad log level": "[logging]\nlevel = \"loud\"",
		"empty dir":     "[music.cache]\ndirectory = \"\"",
	}

	for name, content := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, content)); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}
