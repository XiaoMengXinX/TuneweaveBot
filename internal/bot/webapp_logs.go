package bot

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type logEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
	Caller  string `json:"caller"`
}

const (
	// 看板展示的"最近日志"范围：最近 logMaxFiles 天的日志文件，每个取末尾 logTailBytes，
	// 最多返回 logMaxEntries 条。页面会定时轮询，读取量须有上限。
	logMaxFiles   = 3
	logTailBytes  = 512 << 10
	logMaxEntries = 300
)

var (
	// 对应 logging.Formatter 的输出格式："时间 [级别] 消息 (文件:行)"，消息可以跨行
	logHead   = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}) \[([A-Z]+)\] `)
	logCaller = regexp.MustCompile(` \(([^()\s]+:\d+)\)$`)
)

// readLogs 从日志目录读取最近的日志，新的在前；level 非空时只保留该级别。
// 当天的日志不足时继续读更早的文件。
func readLogs(dir, level string) ([]logEntry, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.log"))
	if err != nil {
		return nil, err
	}
	// 文件名是日期，倒序即从新到旧
	sort.Sort(sort.Reverse(sort.StringSlice(files)))
	files = files[:min(len(files), logMaxFiles)]
	var out []logEntry
	for _, file := range files {
		text, err := readTail(file, logTailBytes)
		if err != nil {
			return out, err
		}
		entries := parseLogs(text)
		for i := len(entries) - 1; i >= 0; i-- {
			if level != "" && entries[i].Level != level {
				continue
			}
			out = append(out, entries[i])
			if len(out) == logMaxEntries {
				return out, nil
			}
		}
	}
	return out, nil
}

func readTail(path string, n int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if info.Size() > n {
		if _, err := f.Seek(-n, io.SeekEnd); err != nil {
			return "", err
		}
	}
	data, err := io.ReadAll(f)
	return string(data), err
}

// parseLogs 按时间顺序解析日志文本；开头被截断、不以时间戳起始的残行丢弃。
func parseLogs(text string) []logEntry {
	var entries []logEntry
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if m := logHead.FindStringSubmatch(line); m != nil {
			entries = append(entries, logEntry{Time: m[1], Level: m[2], Message: line[len(m[0]):]})
		} else if len(entries) > 0 {
			entries[len(entries)-1].Message += "\n" + line
		}
	}
	for i := range entries {
		if m := logCaller.FindStringSubmatch(entries[i].Message); m != nil {
			entries[i].Caller = m[1]
			entries[i].Message = strings.TrimSuffix(entries[i].Message, m[0])
		}
	}
	return entries
}
