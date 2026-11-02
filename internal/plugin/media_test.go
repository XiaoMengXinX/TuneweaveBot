package plugin

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func mediaState(t *testing.T, capable bool) *instanceState {
	t.Helper()
	return &instanceState{
		plugin:       "spotify",
		mediaCapable: capable,
		mediaDir:     t.TempDir(),
	}
}

func mediaCall(t *testing.T, s *instanceState, name string, in any) error {
	t.Helper()
	req, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.dispatchMedia(name, req)
	return err
}

func TestMediaCapabilityGate(t *testing.T) {
	s := mediaState(t, false)
	if err := mediaCall(t, s, "media.open", map[string]string{"ext": "m4a"}); err == nil {
		t.Fatal("expected media call to be rejected without capability")
	}
}

func TestMediaLifecycle(t *testing.T) {
	s := mediaState(t, true)

	raw, err := s.dispatchMedia("media.open", []byte(`{"ext":"m4a"}`))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var open struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &open); err != nil {
		t.Fatal(err)
	}
	token := open.Data.Token
	if len(token) != 32 {
		t.Fatalf("token = %q, want 32 hex chars", token)
	}

	if err := mediaCall(t, s, "media.write", map[string]any{"token": token, "chunk": []byte("hello ")}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := mediaCall(t, s, "media.write", map[string]any{"token": token, "chunk": []byte("world")}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := mediaCall(t, s, "media.close", map[string]string{"token": token}); err != nil {
		t.Fatalf("close: %v", err)
	}

	path := filepath.Join(s.mediaDir, "spotify-"+token+".m4a")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("output = %q, want %q", got, "hello world")
	}
	// tmp 文件应已转正，不留残余
	if _, err := os.Stat(filepath.Join(s.mediaDir, "spotify-"+token+".tmp")); !os.IsNotExist(err) {
		t.Fatal("tmp file should be gone after close")
	}
}

func TestMediaAbort(t *testing.T) {
	s := mediaState(t, true)
	raw, _ := s.dispatchMedia("media.open", []byte(`{"ext":"m4a"}`))
	var open struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	_ = json.Unmarshal(raw, &open)
	token := open.Data.Token

	if err := mediaCall(t, s, "media.write", map[string]any{"token": token, "chunk": []byte("partial")}); err != nil {
		t.Fatal(err)
	}
	if err := mediaCall(t, s, "media.abort", map[string]string{"token": token}); err != nil {
		t.Fatal(err)
	}
	if len(s.mediaSinks) != 0 {
		t.Fatal("abort should drop the sink")
	}
	matches, _ := filepath.Glob(filepath.Join(s.mediaDir, "*"))
	if len(matches) != 0 {
		t.Fatalf("abort should leave no files, found %v", matches)
	}
}

func TestMediaUnknownToken(t *testing.T) {
	s := mediaState(t, true)
	if err := mediaCall(t, s, "media.write", map[string]any{"token": "deadbeef", "chunk": []byte("x")}); err == nil {
		t.Fatal("expected error writing to unknown token")
	}
}

func TestSanitizeExt(t *testing.T) {
	cases := map[string]string{
		"m4a":       "m4a",
		"M4A":       "m4a",
		"mp3":       "mp3",
		"../../etc": "etc",
		"a.b":       "ab",
		"":          "bin",
		"!!!":       "bin",
		"flac":      "flac",
	}
	for in, want := range cases {
		if got := sanitizeExt(in); got != want {
			t.Errorf("sanitizeExt(%q) = %q, want %q", in, got, want)
		}
	}
}
