// Package textx 通用文件名、歌词与 URL 工具。
// 平台相关的链接解析由各 provider 插件自行实现。
package textx

import (
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

var regLrc = regexp.MustCompile(`^(\[[^\]]*\])+`)

func JoinNames(names []string) string {
	return strings.Join(names, "/")
}

var filenameReplacer = strings.NewReplacer(
	"/", " ", "?", " ", "*", " ", ":", " ", "|", " ", "\\", " ", "<", " ", ">", " ", "\"", " ", "\n", " ", "\r", " ",
)

// 常见文件系统限制文件名不超过 255 字节，id3v2 保存时会写入追加 "-id3v2" 的临时文件
const maxFilenameBytes = 255 - len("-id3v2")

// SanitizeFilename 替换非法字符，超长时按字节截断并保留扩展名
func SanitizeFilename(name string) string {
	name = filenameReplacer.Replace(name)
	if len(name) <= maxFilenameBytes {
		return name
	}
	ext := path.Ext(name)
	n := maxFilenameBytes - len(ext)
	for n > 0 && !utf8.RuneStart(name[n]) {
		n--
	}
	return name[:n] + ext
}

// LrcText 去除 LRC 时间标签与空行
func LrcText(lrc string) string {
	var lines []string
	for _, line := range strings.Split(lrc, "\n") {
		if line = strings.TrimSpace(regLrc.ReplaceAllString(line, "")); line != "" {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func URLBase(rawURL string) string {
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		rawURL = rawURL[:i]
	}
	if i := strings.IndexByte(rawURL, '#'); i >= 0 {
		rawURL = rawURL[:i]
	}
	return path.Base(rawURL)
}
