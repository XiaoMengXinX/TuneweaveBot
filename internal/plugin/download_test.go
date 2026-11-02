package plugin

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReleaseDownloads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo-dl-x.bin")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := &instanceState{downloads: []string{path, filepath.Join(dir, "gone.bin")}}

	s.releaseDownloads()

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("download not removed: %v", err)
	}
	if len(s.downloads) != 0 {
		t.Fatalf("downloads not reset: %v", s.downloads)
	}
}

func TestSweepStale(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "stale.mp4")
	fresh := filepath.Join(dir, "fresh.mp4")
	for _, p := range []string{stale, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	sweepStale(dir, time.Minute)

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale file kept: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("fresh file removed: %v", err)
	}
}
