// Command netease 是 TuneweaveBot 的网易云音乐 WASM 插件。
package main

import (
	"encoding/base64"
	"encoding/json"

	"github.com/XiaoMengXinX/TuneweaveBot/pluginsdk"
)

var (
	client *apiClient
	cfg    Config
)

// c-shared reactor 不会执行 main，插件入口须在 init 中注册。
func init() { pluginsdk.Main(handle) }

func main() {}

func manifest() Manifest {
	return Manifest{
		Name:             providerName,
		DisplayName:      displayName,
		Version:          "1.0.0",
		ABI:              pluginsdk.ABI,
		Code:             providerCode,
		Capabilities:     []string{"provider", "recognizer", "auth"},
		AuthRequired:     true,
		DefaultTagFormat: "netease163key",
		Commands: []Command{
			{Name: "netease", Description: "使用网易云音乐点歌"},
			{Name: "program", Description: "按节目 ID 点歌", Type: "template", Template: "https://music.163.com/program?id={args}"},
		},
		Resources: Resources{MemoryMB: 128, Instances: 1},
		HTTPAllowedHosts: []string{
			"music.163.com",
			"interface.music.163.com",
			"163cn.tv",
			"163cn.link",
		},
		ConfigSchema: []ConfigField{
			{Key: "music_u", Type: "string", Default: "", Secret: true, Comment: "网易云登录 Cookie（MUSIC_U），留空则用 /login 扫码或提交 token 登录"},
			{Key: "level", Type: "string", Default: "hires", Comment: "音质: standard/higher/lossless/hires/jyeffect/sky/jymaster，账号无权限时自动降级"},
		},
	}
}

func handle(c *pluginsdk.Context) (any, error) {
	switch c.Action() {
	case "manifest":
		return manifest(), nil
	case "init":
		return initPlugin(c)
	case "shutdown":
		return map[string]any{}, nil
	case "provider.match":
		var in struct {
			Text string `json:"text"`
		}
		if err := c.Decode(&in); err != nil {
			return nil, err
		}
		return map[string]any{"ref": match(in.Text)}, nil
	case "provider.canonicalize":
		var in struct {
			Ref Ref `json:"ref"`
		}
		if err := c.Decode(&in); err != nil {
			return nil, err
		}
		ref, err := canonicalize(in.Ref)
		if err != nil {
			return nil, err
		}
		return map[string]any{"ref": ref}, nil
	case "provider.resolve":
		var in struct {
			Ref Ref `json:"ref"`
		}
		if err := c.Decode(&in); err != nil {
			return nil, err
		}
		return resolve(in.Ref)
	case "provider.search":
		var in struct {
			Keyword string `json:"keyword"`
			Limit   int    `json:"limit"`
			Offset  int    `json:"offset"`
		}
		if err := c.Decode(&in); err != nil {
			return nil, err
		}
		items, total, err := search(in.Keyword, in.Limit, in.Offset)
		if err != nil {
			return nil, err
		}
		return map[string]any{"items": items, "total": total}, nil
	case "provider.lyrics":
		var in struct {
			Ref Ref `json:"ref"`
		}
		if err := c.Decode(&in); err != nil {
			return nil, err
		}
		return lyrics(in.Ref)
	case "recognizer.recognize":
		var in struct {
			Audio string `json:"audio"`
			Mime  string `json:"mime"`
		}
		if err := c.Decode(&in); err != nil {
			return nil, err
		}
		audio, err := base64.StdEncoding.DecodeString(in.Audio)
		if err != nil {
			return nil, pluginsdk.Fail("invalid_request", "invalid audio payload")
		}
		return recognize(c, audio)
	case "auth.status":
		return authStatus()
	case "auth.begin":
		var in struct {
			Mode string `json:"mode"`
		}
		if err := c.Decode(&in); err != nil {
			return nil, err
		}
		return authBegin(in.Mode)
	case "auth.poll":
		var in struct {
			Key string `json:"key"`
		}
		if err := c.Decode(&in); err != nil {
			return nil, err
		}
		return authPoll(in.Key)
	case "auth.submit":
		var in struct {
			Token string `json:"token"`
		}
		if err := c.Decode(&in); err != nil {
			return nil, err
		}
		return authSubmit(in.Token)
	case "auth.apply":
		var in struct {
			Credential string `json:"credential"`
		}
		if err := c.Decode(&in); err != nil {
			return nil, err
		}
		return map[string]any{}, authApply(c, in.Credential)
	default:
		return nil, pluginsdk.Fail("unsupported", "unknown action "+c.Action())
	}
}

func initPlugin(c *pluginsdk.Context) (any, error) {
	var in struct {
		Config     json.RawMessage `json:"config"`
		Credential string          `json:"credential"`
	}
	if err := c.Decode(&in); err != nil {
		return nil, err
	}
	cfg = Config{}
	if len(in.Config) != 0 {
		if err := json.Unmarshal(in.Config, &cfg); err != nil {
			return nil, pluginsdk.Fail("invalid_request", "invalid plugin config")
		}
	}
	if cfg.Level == "" {
		cfg.Level = "hires"
	}
	client = newAPIClient(pluginsdk.NewHTTPClient(), cfg.Level)

	// 凭据优先级：登录持久化的 KV > 配置文件 music_u
	credential := cfg.MusicU
	if value, found, err := c.KVGet("music_u"); err == nil && found {
		credential = string(value)
	}
	if credential != "" {
		client.SetMUSICU(credential)
	}
	return map[string]any{}, nil
}
