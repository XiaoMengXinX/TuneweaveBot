package tagging

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/XiaoMengXinX/163KeyMarker"
	"github.com/bogem/id3v2"
)

func TestWriteEmptyFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	content := []byte("audio")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, "", "", nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(content) {
		t.Errorf("空格式不应修改文件: %q, %v", got, err)
	}
}

func TestWriteUnknownFormat(t *testing.T) {
	err := Write(filepath.Join(t.TempDir(), "song.mp3"), "", "unknown", nil)
	if err == nil || !strings.Contains(err.Error(), "未知标签格式") {
		t.Errorf("err = %v", err)
	}
}

func TestWriteInvalidPayload(t *testing.T) {
	for _, format := range []string{"netease163key", "id3"} {
		t.Run(format, func(t *testing.T) {
			err := Write(filepath.Join(t.TempDir(), "song.mp3"), "", format, json.RawMessage(`not json`))
			if err == nil {
				t.Error("非法 payload 应报错")
			}
		})
	}
}

func TestValidateMarker(t *testing.T) {
	for _, tt := range []struct {
		name string
		raw  string
		ok   bool
	}{
		{name: "valid", raw: `{"musicName":"Song","artist":[["A"]]}`, ok: true},
		{name: "missing name", raw: `{"artist":[["A"]]}`},
		{name: "empty artist entry", raw: `{"musicName":"Song","artist":[[]]}`},
		{name: "non-string artist", raw: `{"musicName":"Song","artist":[[1]]}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var mark marker.MarkerData
			if err := json.Unmarshal([]byte(tt.raw), &mark); err != nil {
				t.Fatal(err)
			}
			err := validateMarker(&mark)
			if (err == nil) != tt.ok {
				t.Errorf("validateMarker error = %v, want ok %v", err, tt.ok)
			}
		})
	}
}

func TestWriteID3MP3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.mp3")
	// 仅含空 ID3v2 头的文件，足以让 id3v2 解析并写回标签
	if err := os.WriteFile(path, []byte("ID3\x04\x00\x00\x00\x00\x00\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(Metadata{Title: "Song", Artists: []string{"A", "B"}, Album: "Album"})
	if err := Write(path, "", "id3", payload); err != nil {
		t.Fatal(err)
	}
	tag, err := id3v2.Open(path, id3v2.Options{Parse: true})
	if err != nil {
		t.Fatal(err)
	}
	defer tag.Close()
	if tag.Title() != "Song" || tag.Artist() != "A, B" || tag.Album() != "Album" {
		t.Errorf("tags = %q / %q / %q", tag.Title(), tag.Artist(), tag.Album())
	}
}

func TestWriteID3SkipsUnknownExtension(t *testing.T) {
	path := filepath.Join(t.TempDir(), "song.ogg")
	if err := os.WriteFile(path, []byte("audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(Metadata{Title: "Song"})
	if err := Write(path, "", "id3", payload); err != nil {
		t.Fatal(err)
	}
}
