package plugin

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// maxMediaBytes 是插件单个媒体文件（media.write / media.download）的上限，
// 取 Local Bot API Server 的上传上限：更大的文件 bot 发不出去，落盘只会白占磁盘。
const maxMediaBytes int64 = 2 << 30

// maxOpenMediaSinks 单实例同时打开的写入会话上限。
const maxOpenMediaSinks = 4

// mediaSink 是一次插件媒体写入会话：落盘到临时文件，close 后原地转正。
type mediaSink struct {
	token   string
	ext     string
	tmpPath string
	path    string
	file    *os.File
	written int64
}

// dispatchMedia 处理 media.open/write/close/abort，仅在插件声明 media 能力时可用。
func (s *instanceState) dispatchMedia(name string, req []byte) ([]byte, error) {
	if !s.mediaCapable {
		return nil, fmt.Errorf("plugin %s lacks media capability", s.plugin)
	}
	switch name {
	case "media.open":
		var in struct {
			Ext string `json:"ext"`
		}
		if err := json.Unmarshal(req, &in); err != nil {
			return nil, err
		}
		token, err := s.mediaOpen(in.Ext)
		if err != nil {
			return nil, err
		}
		return okResponse(map[string]any{"token": token})

	case "media.write":
		var in struct {
			Token string `json:"token"`
			Chunk []byte `json:"chunk"`
		}
		if err := json.Unmarshal(req, &in); err != nil {
			return nil, err
		}
		if err := s.mediaWrite(in.Token, in.Chunk); err != nil {
			return nil, err
		}
		return okResponse(map[string]any{})

	case "media.close":
		var in struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(req, &in); err != nil {
			return nil, err
		}
		if err := s.mediaClose(in.Token); err != nil {
			return nil, err
		}
		return okResponse(map[string]any{})

	case "media.abort":
		var in struct {
			Token string `json:"token"`
		}
		if err := json.Unmarshal(req, &in); err != nil {
			return nil, err
		}
		s.mediaAbort(in.Token)
		return okResponse(map[string]any{})
	}
	return nil, fmt.Errorf("unknown media call %q", name)
}

func (s *instanceState) mediaOpen(ext string) (string, error) {
	if len(s.mediaSinks) >= maxOpenMediaSinks {
		return "", fmt.Errorf("too many open media sinks")
	}
	ext = sanitizeExt(ext)
	if err := os.MkdirAll(s.mediaDir, 0o755); err != nil {
		return "", err
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw[:])
	base := filepath.Join(s.mediaDir, s.plugin+"-"+token)
	path := base + "." + ext
	f, err := os.Create(base + ".tmp")
	if err != nil {
		return "", err
	}
	if s.mediaSinks == nil {
		s.mediaSinks = map[string]*mediaSink{}
	}
	s.mediaSinks[token] = &mediaSink{
		token:   token,
		ext:     ext,
		tmpPath: base + ".tmp",
		path:    path,
		file:    f,
	}
	return token, nil
}

func (s *instanceState) mediaWrite(token string, chunk []byte) error {
	sink, ok := s.mediaSinks[token]
	if !ok {
		return fmt.Errorf("unknown media token")
	}
	if sink.written+int64(len(chunk)) > maxMediaBytes {
		s.mediaAbort(token)
		return fmt.Errorf("media write exceeds %d bytes", maxMediaBytes)
	}
	n, err := sink.file.Write(chunk)
	sink.written += int64(n)
	return err
}

func (s *instanceState) mediaClose(token string) error {
	sink, ok := s.mediaSinks[token]
	if !ok {
		return fmt.Errorf("unknown media token")
	}
	delete(s.mediaSinks, token)
	if err := sink.file.Close(); err != nil {
		_ = os.Remove(sink.tmpPath)
		return err
	}
	if sink.written == 0 {
		_ = os.Remove(sink.tmpPath)
		return fmt.Errorf("media closed with no data")
	}
	return os.Rename(sink.tmpPath, sink.path)
}

func (s *instanceState) mediaAbort(token string) {
	sink, ok := s.mediaSinks[token]
	if !ok {
		return
	}
	delete(s.mediaSinks, token)
	_ = sink.file.Close()
	_ = os.Remove(sink.tmpPath)
}

// closeMedia 丢弃实例时清掉所有未完成的写入会话。
func (s *instanceState) closeMedia() {
	for token := range s.mediaSinks {
		s.mediaAbort(token)
	}
}

// sanitizeExt 把扩展名限制为小写字母数字，缺省回落到 bin。
func sanitizeExt(ext string) string {
	out := make([]byte, 0, len(ext))
	for _, c := range []byte(ext) {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			out = append(out, c)
		case c >= 'A' && c <= 'Z':
			out = append(out, c+('a'-'A'))
		}
	}
	if len(out) == 0 {
		return "bin"
	}
	return string(out)
}
