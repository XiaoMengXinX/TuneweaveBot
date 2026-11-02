package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/plugin"
)

// GenerateTemplate 生成带注释的配置模板：先输出宿主各段的默认值，
// 再按插件声明的 config_schema 逐段输出 [plugins.<name>]。
func GenerateTemplate(schemas []plugin.PluginConfigSchema) []byte {
	d := Default()
	var b strings.Builder

	b.WriteString("# TuneweaveBot 配置模板（由 gen-config 生成）\n")
	b.WriteString("# 所有配置项都可用环境变量覆盖，如 TUNEWEAVE_BOT_TOKEN、TUNEWEAVE_DOWNLOAD_TIMEOUT\n\n")

	b.WriteString("[bot]\n")
	b.WriteString("token = \"\"                              # 必填，BotFather 给的 token\n")
	fmt.Fprintf(&b, "api_url = %s          # 自建 Bot API 时修改\n", strconv.Quote(d.Bot.APIURL))
	b.WriteString("local_mode = false                      # API 开启 --local 且 bot 可读取其文件目录时启用\n")
	fmt.Fprintf(&b, "upload_timeout = %s                 # 上传及其他 Telegram API 请求的总超时，须为正时长\n", tomlDuration(d.Bot.UploadTimeout))
	b.WriteString("admin = []                              # 管理员用户 ID，可用 /rmcache /reload\n")
	b.WriteString("cache_chat = 0                          # 缓存群 ID，Guest Mode 点歌时先把未缓存的歌曲上传到这里；0 为关闭，可用 /setcachechat 设置\n")
	b.WriteString("debug = false                           # 在终端输出 SDK 调试日志，[log] level 为 debug 时同时写入日志文件\n\n")

	b.WriteString("[plugins]\n")
	fmt.Fprintf(&b, "dir = %s                   # .wasm 插件目录，构建后由 build-plugins.sh 复制到这里\n", strconv.Quote(d.Plugins.Dir))
	b.WriteString("enabled = []                            # 要加载的插件名（文件名去掉 .wasm）；留空 = 加载目录下全部\n")
	b.WriteString("default = \"\"                             # 默认 provider，/music、/search、/login 未指定时使用\n")
	fmt.Fprintf(&b, "instances = %d                           # 每个插件常驻的实例数，处理搜索/匹配等轻量调用；可在 [plugins.<name>] 中用 instances 单独覆盖\n", d.Plugins.Instances)
	fmt.Fprintf(&b, "call_timeout = %s                    # 单次插件调用超时\n", tomlDuration(d.Plugins.CallTimeout))
	fmt.Fprintf(&b, "memory_mb = %d                         # 单个插件实例内存上限\n", d.Plugins.MemoryMB)
	fmt.Fprintf(&b, "http_max_bytes = %d              # 插件单次 HTTP 响应大小上限\n", d.Plugins.HTTPMaxBytes)
	b.WriteString("allow_private_networks = false          # 是否允许插件访问/下载私网地址（默认拒绝，防止 SSRF）\n")
	b.WriteString("proxy = \"\"                               # 所有插件 HTTP 及宿主下载共用的代理，留空保留环境代理\n")

	for _, s := range schemas {
		b.WriteString("\n")
		if s.DisplayName != "" {
			fmt.Fprintf(&b, "# %s\n", s.DisplayName)
		}
		fmt.Fprintf(&b, "[plugins.%s]\n", s.Name)
		for _, f := range s.Fields {
			writeField(&b, f)
		}
	}

	b.WriteString("\n[database]\n")
	fmt.Fprintf(&b, "path = %s                       # SQLite 数据库路径\n", strconv.Quote(d.Database.Path))

	b.WriteString("\n[download]\n")
	fmt.Fprintf(&b, "timeout = %s                        # 单次下载请求超时\n", tomlDuration(d.Download.Timeout))
	fmt.Fprintf(&b, "retries = %d                             # 下载失败后的重试次数\n", d.Download.Retries)
	fmt.Fprintf(&b, "concurrency = %d                         # 同时进行的下载/识曲任务数，每个任务占用一个临时插件实例\n", d.Download.Concurrency)

	b.WriteString("\n[log]\n")
	fmt.Fprintf(&b, "level = %s                          # trace/debug/info/warn/error\n", strconv.Quote(d.Log.Level))
	fmt.Fprintf(&b, "path = %s                        # 日志目录，按日期切分\n", strconv.Quote(d.Log.Path))

	return []byte(b.String())
}

// writeField 输出单个插件配置项；secret 字段只写空值。
func writeField(b *strings.Builder, f plugin.ConfigField) {
	value := tomlDefault(f)
	if f.Secret {
		value = defaultLiteral(f.Type)
	}
	if f.Comment != "" {
		fmt.Fprintf(b, "%s = %s # %s\n", f.Key, value, f.Comment)
	} else {
		fmt.Fprintf(b, "%s = %s\n", f.Key, value)
	}
}

// tomlDefault 把配置项的默认值渲染成 TOML 字面量；缺省时用该类型零值。
func tomlDefault(f plugin.ConfigField) string {
	switch f.Type {
	case "string":
		if s, ok := f.Default.(string); ok {
			return strconv.Quote(s)
		}
	case "bool":
		if v, ok := f.Default.(bool); ok {
			return strconv.FormatBool(v)
		}
	case "int":
		if n, ok := f.Default.(float64); ok {
			return strconv.FormatInt(int64(n), 10)
		}
	}
	return defaultLiteral(f.Type)
}

func defaultLiteral(typ string) string {
	switch typ {
	case "bool":
		return "false"
	case "int":
		return "0"
	default:
		return `""`
	}
}

func tomlDuration(d Duration) string {
	text, _ := d.MarshalText()
	return strconv.Quote(string(text))
}
