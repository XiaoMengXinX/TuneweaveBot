package plugin

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	maxVersionInfoBytes = 64 << 10
	maxPluginWasmBytes  = 256 << 20
)

// ErrUpToDate 表示插件已是最新版本。
var ErrUpToDate = errors.New("插件已是最新版本")

// VersionInfo 是插件发布的 version.json，也用于描述当前加载的版本。
type VersionInfo struct {
	Version   string `json:"version"`
	BuildTime string `json:"build_time"`
	Commit    string `json:"commit"`
	SHA256    string `json:"sha256,omitempty"` // wasm 文件的 sha256（十六进制）
}

// Update 描述一个插件的可用更新。
type Update struct {
	Name        string
	DisplayName string
	Current     VersionInfo
	Latest      VersionInfo
}

func currentVersion(m Manifest) VersionInfo {
	return VersionInfo{Version: m.Version, BuildTime: m.BuildTime, Commit: m.Commit}
}

// CheckUpdates 检查所有声明了更新地址的插件。checked 为参与检查的插件数，
// 单个插件检查失败不影响其他插件，错误合并返回。
func (m *Manager) CheckUpdates(ctx context.Context) (updates []Update, checked int, err error) {
	if m == nil {
		return nil, 0, nil
	}
	var errs []error
	for _, name := range m.order {
		p := m.plugins[name]
		if p.manifest.VersionURL == "" {
			continue
		}
		checked++
		latest, err := m.fetchVersion(ctx, p.manifest.VersionURL)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			continue
		}
		if current := currentVersion(p.manifest); isNewer(latest, current) {
			updates = append(updates, Update{Name: name, DisplayName: p.manifest.DisplayName, Current: current, Latest: latest})
		}
	}
	return updates, checked, errors.Join(errs...)
}

// ApplyUpdate 下载插件的最新 wasm，校验其 manifest 与 version.json 一致后原地替换文件。
// 正在运行的实例不受影响，新文件在下次加载插件时生效。
func (m *Manager) ApplyUpdate(ctx context.Context, name string) (Update, error) {
	p, ok := m.plugins[name]
	if !ok || p.manifest.UpdateURL == "" {
		return Update{}, fmt.Errorf("插件 %s 没有声明更新地址", name)
	}
	latest, err := m.fetchVersion(ctx, p.manifest.VersionURL)
	if err != nil {
		return Update{}, err
	}
	current := currentVersion(p.manifest)
	if !isNewer(latest, current) {
		return Update{}, ErrUpToDate
	}
	raw, err := m.fetch(ctx, p.manifest.UpdateURL, maxPluginWasmBytes)
	if err != nil {
		return Update{}, err
	}
	if latest.SHA256 != "" {
		if sum := sha256.Sum256(raw); !strings.EqualFold(hex.EncodeToString(sum[:]), latest.SHA256) {
			return Update{}, errors.New("下载的插件 sha256 与 version.json 不一致")
		}
	}
	wasm, err := decompressWasm(raw)
	if err != nil {
		return Update{}, err
	}
	got, err := m.runtime.inspect(ctx, wasm, name)
	if err != nil {
		return Update{}, err
	}
	if got.Name != p.manifest.Name || got.Version != latest.Version || got.BuildTime != latest.BuildTime {
		return Update{}, fmt.Errorf("下载的插件（%s %s %s）与 version.json 不一致", got.Name, got.Version, got.BuildTime)
	}
	if err := backupFile(p.path); err != nil {
		return Update{}, fmt.Errorf("备份旧插件失败: %w", err)
	}
	data, err := encodeForPath(p.path, raw, wasm)
	if err != nil {
		return Update{}, err
	}
	if err := replaceFile(p.path, data); err != nil {
		return Update{}, err
	}
	return Update{Name: name, DisplayName: p.manifest.DisplayName, Current: current, Latest: latest}, nil
}

func (m *Manager) fetchVersion(ctx context.Context, url string) (VersionInfo, error) {
	body, err := m.fetch(ctx, url, maxVersionInfoBytes)
	if err != nil {
		return VersionInfo{}, err
	}
	var v VersionInfo
	if err := json.Unmarshal(body, &v); err != nil {
		return VersionInfo{}, fmt.Errorf("解析 version.json 失败: %w", err)
	}
	if v.Version == "" {
		return VersionInfo{}, errors.New("version.json 缺少 version")
	}
	return v, nil
}

// updateClient 复用插件 HTTP 客户端的代理与私网拦截，但会跟随重定向：
// 发布文件常经 302 跳转到实际存储（如 GitHub Releases），每一跳仍经过私网检查。
func (m *Manager) updateClient() *http.Client {
	c := *m.runtime.httpClient
	c.CheckRedirect = nil
	return &c
}

func (m *Manager) fetch(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.updateClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("下载 %s 失败: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s 超过 %d 字节上限", url, limit)
	}
	return body, nil
}

// encodeForPath 按现有文件的格式（.wasm.gz 或 .wasm）生成要写入的内容，
// 使备份与回滚始终针对同一路径。
func encodeForPath(path string, raw, wasm []byte) ([]byte, error) {
	if !strings.HasSuffix(strings.ToLower(path), ".gz") {
		return wasm, nil
	}
	if isGzip(raw) {
		return raw, nil
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if _, err := zw.Write(wasm); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// backupPath 是更新前旧插件的备份位置；新版本加载失败时用它回滚。
func backupPath(path string) string { return path + ".bak" }

// backupFile 备份当前插件文件。已有备份说明上次更新尚未成功加载，
// 保留那份最后可用的版本，不用未验证的文件覆盖它。
func backupFile(path string) error {
	if _, err := os.Stat(backupPath(path)); err == nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return replaceFile(backupPath(path), data)
}

// replaceFile 先写同目录临时文件再重命名，替换过程中不会出现半个文件。
func replaceFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// isNewer 版本号可按数字比较且不同时，只有远端更大才算有更新；版本号相同或无法比较时
// 比较构建时间，任一方缺少构建时间视为无更新。
func isNewer(latest, current VersionInfo) bool {
	if c, ok := compareVersions(latest.Version, current.Version); ok && c != 0 {
		return c > 0
	}
	lt, err1 := time.Parse(time.RFC3339, latest.BuildTime)
	ct, err2 := time.Parse(time.RFC3339, current.BuildTime)
	return err1 == nil && err2 == nil && lt.After(ct)
}

// compareVersions 按点分数字比较版本号（可带 v 前缀），缺失的段视为 0。
func compareVersions(a, b string) (int, bool) {
	pa, ok1 := parseVersion(a)
	pb, ok2 := parseVersion(b)
	if !ok1 || !ok2 {
		return 0, false
	}
	for i := range max(len(pa), len(pb)) {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x > y {
				return 1, true
			}
			return -1, true
		}
	}
	return 0, true
}

func parseVersion(v string) ([]int, bool) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	out := make([]int, len(parts))
	for i, s := range parts {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}
