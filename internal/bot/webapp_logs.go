package bot

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type logEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Message string `json:"message"`
	Caller  string `json:"caller"`
}

const (
	// 面板每次展示一天的日志：取该日文件末尾 logTailBytes，最多返回 logMaxEntries 条。
	// 页面开启实时刷新时会定时轮询，读取量须有上限。
	logTailBytes  = 512 << 10
	logMaxEntries = 300
)

var (
	// 对应 logging.Formatter 的输出格式："时间 [级别] 消息 (文件:行)"，消息可以跨行
	logHead   = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}) \[([A-Z]+)\] `)
	logCaller = regexp.MustCompile(` \(([^()\s]+:\d+)\)$`)
)

// logDay 匹配日志文件名中的日期；也用来校验前端传入的日期，避免拼出目录之外的路径。
var logDay = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// logDays 列出有日志文件的日期，新的在前。
func logDays(dir string) ([]string, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var days []string
	for _, f := range files {
		if day, ok := strings.CutSuffix(f.Name(), ".log"); ok && logDay.MatchString(day) {
			days = append(days, day)
		}
	}
	slices.Sort(days)
	slices.Reverse(days)
	return days, nil
}

// readLogs 读取某一天的日志，新的在前；level 非空时只保留该级别。当天没有日志文件时返回空。
func readLogs(dir, day, level string) ([]logEntry, error) {
	text, err := readTail(filepath.Join(dir, day+".log"), logTailBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []logEntry
	entries := parseLogs(text)
	for i := len(entries) - 1; i >= 0 && len(out) < logMaxEntries; i-- {
		if level == "" || entries[i].Level == level {
			out = append(out, entries[i])
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
