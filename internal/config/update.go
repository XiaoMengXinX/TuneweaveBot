package config

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

var (
	sectionLine   = regexp.MustCompile(`^\s*\[\s*([^\[\]\s]+)\s*\]\s*(#.*)?$`)
	cacheChatLine = regexp.MustCompile(`^(\s*cache_chat\s*=\s*)[+-]?\d+(.*)$`)
)

// keyLine 匹配 [plugins.<section>] 段内指定键的赋值行，保留行尾注释。
func keyLine(key string) *regexp.Regexp {
	return regexp.MustCompile(`^(\s*` + regexp.QuoteMeta(key) + `\s*=\s*)('[^']*'|"[^"]*"|\S*)(\s*(?:#.*)?)$`)
}

// UpdatePluginValue 修改 [plugins.<section>] 段内的某个字符串键，缺段或缺键时补齐，
// 其余行（含注释）原样保留，原子写入。
func UpdatePluginValue(path, section, key, value string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	target := "plugins." + section
	kv := keyLine(key)

	inSection, sectionLineIdx := false, -1
	for i, line := range lines {
		if m := sectionLine.FindStringSubmatch(line); m != nil {
			if inSection {
				// 离开目标段仍未找到键：在段尾前插入
				lines = slices.Insert(lines, i, key+" = "+strconv.Quote(value))
				return atomicWrite(path, []byte(strings.Join(lines, "\n")))
			}
			inSection = m[1] == target
			if inSection {
				sectionLineIdx = i
			}
			continue
		}
		if !inSection {
			continue
		}
		if m := kv.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + strconv.Quote(value) + m[3]
			return atomicWrite(path, []byte(strings.Join(lines, "\n")))
		}
	}
	if sectionLineIdx >= 0 {
		lines = slices.Insert(lines, sectionLineIdx+1, key+" = "+strconv.Quote(value))
		return atomicWrite(path, []byte(strings.Join(lines, "\n")))
	}
	lines = append(lines, "", "[plugins."+section+"]", key+" = "+strconv.Quote(value))
	return atomicWrite(path, []byte(strings.Join(lines, "\n")))
}

// UpdateCacheChat 修改 [bot] 中的 cache_chat，缺少时插入到段首
func UpdateCacheChat(path string, chatID int64) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.Split(string(data), "\n")
	value := strconv.FormatInt(chatID, 10)
	section, botLine := "", -1
	for i, line := range lines {
		if m := sectionLine.FindStringSubmatch(line); m != nil {
			section = m[1]
			if section == "bot" {
				botLine = i
			}
			continue
		}
		if section != "bot" {
			continue
		}
		if m := cacheChatLine.FindStringSubmatch(line); m != nil {
			lines[i] = m[1] + value + m[2]
			return atomicWrite(path, []byte(strings.Join(lines, "\n")))
		}
	}
	if botLine < 0 {
		return fmt.Errorf("配置文件 %s 中未找到 [bot] 配置段", path)
	}
	lines = slices.Insert(lines, botLine+1, "cache_chat = "+value)
	return atomicWrite(path, []byte(strings.Join(lines, "\n")))
}
