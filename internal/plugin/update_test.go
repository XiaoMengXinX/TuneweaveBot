package plugin

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"gorm.io/gorm/logger"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
)

func TestIsNewer(t *testing.T) {
	cur := VersionInfo{Version: "1.2.0", BuildTime: "2026-01-01T00:00:00Z"}
	for _, c := range []struct {
		latest VersionInfo
		want   bool
	}{
		{VersionInfo{Version: "1.2.1"}, true},
		{VersionInfo{Version: "v1.10"}, true},
		{VersionInfo{Version: "1.1.9", BuildTime: "2027-01-01T00:00:00Z"}, false}, // 远端版本号更低
		{VersionInfo{Version: "1.2.0", BuildTime: "2026-01-02T00:00:00Z"}, true},
		{VersionInfo{Version: "1.2.0", BuildTime: "2026-01-01T00:00:00Z"}, false},
		{VersionInfo{Version: "1.2.0"}, false},
		{VersionInfo{Version: "v1.2"}, false}, // 数字相同，比较构建时间
		{VersionInfo{Version: "1.2.0-beta"}, false},
		{VersionInfo{Version: "1.2.0-beta", BuildTime: "2026-01-02T00:00:00Z"}, true}, // 无法比较时看构建时间
	} {
		if got := isNewer(c.latest, cur); got != c.want {
			t.Errorf("isNewer(%+v) = %v, want %v", c.latest, got, c.want)
		}
	}
	if isNewer(VersionInfo{Version: "1.2.0", BuildTime: "2026-01-02T00:00:00Z"}, VersionInfo{Version: "1.2.0"}) {
		t.Error("本地缺少构建时间时不应视为有更新")
	}
}

// TestApplyUpdate 构建新旧两版 netease 插件，验证检查、校验与原地替换。
func TestApplyUpdate(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	const (
		oldBuild = "2026-01-01T00:00:00Z"
		newBuild = "2026-02-01T00:00:00Z"
	)
	versionJSON := []byte(`{"version":"1.0.0","build_time":"` + newBuild + `","commit":"abc"}`)
	var newWasm []byte
	// 与 GitHub Releases 一样，发布地址先 302 跳转到实际存储
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/version.json", "/netease.wasm.gz":
			http.Redirect(w, r, "/storage"+r.URL.Path, http.StatusFound)
		case "/storage/version.json":
			_, _ = w.Write(versionJSON)
		case "/storage/netease.wasm.gz":
			_, _ = w.Write(newWasm)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	// build 构建插件并返回 gzip 压缩后的内容，与发布产物 .wasm.gz 一致
	build := func(buildTime string) []byte {
		t.Helper()
		root, _ := filepath.Abs("../..")
		out := filepath.Join(t.TempDir(), "netease.wasm")
		ldflags := "-s -w -X main.buildTime=" + buildTime +
			" -X main.updateURL=" + srv.URL + "/netease.wasm.gz -X main.versionURL=" + srv.URL + "/version.json"
		cmd := exec.Command("go", "build", "-buildmode=c-shared", "-trimpath", "-ldflags="+ldflags, "-o", out, "./plugins/netease")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=wasip1", "GOARCH=wasm")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build netease: %v\n%s", err, b)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		_, _ = zw.Write(data)
		_ = zw.Close()
		return buf.Bytes()
	}
	wasmPath := filepath.Join(dir, "netease.wasm.gz")
	oldWasm := build(oldBuild)
	newWasm = build(newBuild)
	if err := os.WriteFile(wasmPath, oldWasm, 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "cache.db"), logger.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	load := func() *Manager {
		t.Helper()
		m, err := Load(context.Background(), Config{
			Dir:     dir,
			Default: "netease",
			Runtime: RuntimeConfig{AllowPrivateNetworks: true, ScratchDir: t.TempDir(), MediaDir: t.TempDir()},
		}, st, log, nil)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	ctx := context.Background()

	m := load()
	updates, checked, err := m.CheckUpdates(ctx)
	if err != nil || checked != 1 || len(updates) != 1 || updates[0].Latest.BuildTime != newBuild || updates[0].Current.BuildTime != oldBuild {
		t.Fatalf("CheckUpdates = %+v, %d, %v", updates, checked, err)
	}

	// version.json 与下载到的 wasm 不一致时拒绝替换
	versionJSON = []byte(`{"version":"1.0.0","build_time":"2026-03-01T00:00:00Z"}`)
	if _, err := m.ApplyUpdate(ctx, "netease"); err == nil {
		t.Fatal("不一致的更新应被拒绝")
	}
	if got, _ := os.ReadFile(wasmPath); !bytes.Equal(got, oldWasm) {
		t.Fatal("校验失败后文件不应被替换")
	}

	// sha256 与下载内容不一致时拒绝替换
	versionJSON = []byte(`{"version":"1.0.0","build_time":"` + newBuild + `","sha256":"` + strings.Repeat("0", 64) + `"}`)
	if _, err := m.ApplyUpdate(ctx, "netease"); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("sha256 不一致时应拒绝: %v", err)
	}

	sum := sha256.Sum256(newWasm)
	versionJSON = []byte(`{"version":"1.0.0","build_time":"` + newBuild + `","sha256":"` + hex.EncodeToString(sum[:]) + `"}`)
	u, err := m.ApplyUpdate(ctx, "netease")
	if err != nil || u.Latest.BuildTime != newBuild {
		t.Fatalf("ApplyUpdate = %+v, %v", u, err)
	}
	if got, _ := os.ReadFile(wasmPath); !bytes.Equal(got, newWasm) {
		t.Fatal("文件未替换为新版本")
	}
	if got, _ := os.ReadFile(backupPath(wasmPath)); !bytes.Equal(got, oldWasm) {
		t.Fatal("更新前的版本未备份")
	}
	m.Close(ctx)

	m = load()
	defer m.Close(ctx)
	if info := m.Info(); info[0].BuildTime != newBuild || len(m.Rollbacks()) != 0 {
		t.Fatalf("重新加载后 build_time = %q, rollbacks = %+v", info[0].BuildTime, m.Rollbacks())
	}
	if _, err := os.Stat(backupPath(wasmPath)); !os.IsNotExist(err) {
		t.Fatal("新版本加载成功后应删除备份")
	}
	if updates, _, err := m.CheckUpdates(ctx); err != nil || len(updates) != 0 {
		t.Fatalf("更新后仍报告更新: %+v, %v", updates, err)
	}
	if _, err := m.ApplyUpdate(ctx, "netease"); err != ErrUpToDate {
		t.Fatalf("ApplyUpdate on latest = %v", err)
	}
}

// TestRollbackOnLoadFailure 新版本无法加载时恢复备份并记录回滚。
func TestRollbackOnLoadFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	root, _ := filepath.Abs("../..")
	dir := t.TempDir()
	wasmPath := filepath.Join(dir, "example.wasm")
	cmd := exec.Command("go", "build", "-buildmode=c-shared", "-trimpath", "-ldflags=-s -w", "-o", wasmPath, "./plugins/example")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=wasip1", "GOARCH=wasm")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build example: %v\n%s", err, b)
	}
	good, _ := os.ReadFile(wasmPath)
	if err := os.WriteFile(backupPath(wasmPath), good, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wasmPath, []byte("not a wasm module"), 0o644); err != nil {
		t.Fatal(err)
	}

	st, err := store.Open(filepath.Join(t.TempDir(), "cache.db"), logger.Discard)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	m, err := Load(context.Background(), Config{
		Dir: dir, Default: "example",
		Runtime: RuntimeConfig{ScratchDir: t.TempDir(), MediaDir: t.TempDir()},
	}, st, log, nil)
	if err != nil {
		t.Fatalf("回滚后应能加载: %v", err)
	}
	defer m.Close(context.Background())
	if rbs := m.Rollbacks(); len(rbs) != 1 || rbs[0].Name != "example" || rbs[0].Err == nil {
		t.Fatalf("rollbacks = %+v", rbs)
	}
	if got, _ := os.ReadFile(wasmPath); !bytes.Equal(got, good) {
		t.Fatal("插件文件未恢复为备份")
	}
	if _, err := os.Stat(backupPath(wasmPath)); !os.IsNotExist(err) {
		t.Fatal("回滚后备份应已用掉")
	}
}

func TestPluginFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.wasm", "b.wasm.gz", "a.wasm.bak", ".b.wasm.gz.123.tmp", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := pluginFiles(dir)
	if err != nil || len(files) != 2 || filepath.Base(files[0]) != "a.wasm" || filepath.Base(files[1]) != "b.wasm.gz" {
		t.Fatalf("pluginFiles = %v, %v", files, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.wasm.gz"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := pluginFiles(dir); err == nil || !strings.Contains(err.Error(), "同时存在") {
		t.Fatalf("同名插件应报错: %v", err)
	}
}

func TestEncodeForPath(t *testing.T) {
	wasm := []byte("\x00asm plugin")
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write(wasm)
	_ = zw.Close()
	gz := buf.Bytes()

	if got, _ := encodeForPath("p.wasm", gz, wasm); !bytes.Equal(got, wasm) {
		t.Error(".wasm 应写入解压后的内容")
	}
	if got, _ := encodeForPath("p.wasm.gz", gz, wasm); !bytes.Equal(got, gz) {
		t.Error(".wasm.gz 应直接写入下载的压缩内容")
	}
	got, err := encodeForPath("p.wasm.gz", wasm, wasm)
	if err != nil {
		t.Fatal(err)
	}
	if back, err := decompressWasm(got); err != nil || !bytes.Equal(back, wasm) {
		t.Errorf("未压缩的下载写入 .wasm.gz 前应压缩: %v", err)
	}
}
