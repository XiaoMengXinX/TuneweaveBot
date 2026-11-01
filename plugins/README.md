# 插件开发指南

TuneweaveBot 把音乐平台相关能力全部放在 WASM 插件里。宿主（`internal/plugin`）
负责加载 `.wasm`、实例池、超时/内存限制、HTTP 代理、KV 存储，并把插件能力
包装成 `provider.Provider`、`provider.Recognizer`、`provider.Authenticator`。

插件是独立的 Go module，通过 `pluginsdk` 注册一个处理函数即可，无需了解宿主内部。

## 目录

```text
pluginsdk/            插件 SDK（独立 module）
plugins/netease/      官方网易云插件（provider + recognizer + auth）
plugins/example/      最小 provider 示例
dist/plugins/         构建产物（不进版本库）
plugins/*.wasm        运行时加载目录（不进版本库）
```

`go.work` 已包含 `.`、`./pluginsdk`、`./plugins/netease`、`./plugins/example`。
新插件请在自己的 `go.mod` 中加：

```go
require github.com/XiaoMengXinX/TuneweaveBot/pluginsdk v0.0.0

replace github.com/XiaoMengXinX/TuneweaveBot/pluginsdk => ../../pluginsdk
```

## 构建

```sh
bash build-plugins.sh                      # 构建全部（当前为 netease）
PLUGINS="example" bash build-plugins.sh    # 指定插件
NO_COPY=1 bash build-plugins.sh            # 只输出到 dist/plugins，不复制到 plugins/
```

实际命令：

```sh
CGO_ENABLED=0 GOOS=wasip1 GOARCH=wasm \
  go build -buildmode=c-shared -trimpath -ldflags="-s -w" \
  -o dist/plugins/<name>.wasm ./plugins/<name>
```

文件名（去掉 `.wasm`）用于匹配 `[plugins] enabled`，插件内部通过 manifest 的
`name` 注册为 provider；两者通常保持一致。默认 provider 缺失或目录为空时宿主拒绝启动。

## ABI v1

### 导出（插件 → 宿主）

| 导出 | 说明 |
| --- | --- |
| `_initialize` | Go `c-shared` reactor 自带，宿主实例化时调用 |
| `alloc(size u32) -> u32` | 分配并持有内存（SDK 用 map 固定，避免 GC 移动） |
| `dealloc(ptr u32, size u32)` | 释放 `alloc`/`handle` 返回的内存 |
| `handle(ptr u32, len u32) -> u64` | 处理一次调用，返回 `ptr \| len<<32` |

> Go 的 reactor 模块不会执行 `main()`，插件必须在 `init()` 里注册：
>
> ```go
> func init() { pluginsdk.Main(handle) }
> func main() {}
> ```

### 导入（宿主 → 插件）

宿主导出模块 `host` 的唯一函数：

```text
host.call(namePtr, nameLen, reqPtr, reqLen, respPtr, respCap) -> i64
```

- `>= 0`：写入 `respPtr` 的字节数
- `-2`：缓冲区不足；宿主在 `respPtr` 前 8 字节写入所需大小，插件扩容后重试
- `-1`：其他错误

host 函数**不会回调插件的 `alloc`**；响应缓冲区由插件预先分配并传入。
`pluginsdk` 已处理这套协议，插件作者直接调用 `Context` 方法即可。

### 信封

请求与响应都是 UTF-8 JSON，二进制字段用 base64：

```json
{"abi":1,"action":"provider.resolve","payload":{"ref":{"id":"123"}}}
{"ok":true,"data":{"track":{"title":"..."}}}
{"ok":false,"error":{"code":"not_found","message":"song 123"}}
```

错误码：`no_result`、`unsupported`、`auth_required`、`rate_limited`、`not_found`、
`invalid_request`、`internal`。宿主据此映射用户文案；`message` 只写日志，不直接展示。

## Actions

| Action | 方向 | Payload → Data |
| --- | --- | --- |
| `manifest` | 宿主 → 插件 | `{}` → manifest |
| `init` | 宿主 → 插件 | `{config, credential}` → `null` |
| `shutdown` | 宿主 → 插件 | `{}` → `null` |
| `provider.match` | 宿主 → 插件 | `{text}` → `{ref}` 或 `{ref:null}`；不得做重网络请求 |
| `provider.canonicalize` | 宿主 → 插件 | `{ref}` → `{ref}`；节目等子资源归一为可缓存歌曲 |
| `provider.resolve` | 宿主 → 插件 | `{ref}` → `{track, no_audio?}` |
| `provider.search` | 宿主 → 插件 | `{keyword,limit,offset}` → `{items,total}` |
| `provider.lyrics` | 宿主 → 插件 | `{ref}` → `{lrc,title,artists,album}` |
| `recognizer.recognize` | 宿主 → 插件 | `{audio(b64),mime}` → `{ref}` |
| `auth.status` | 宿主 → 插件 | `{}` → `{authenticated,account}` |
| `auth.begin` | 宿主 → 插件 | `{}` → `{type,content,key,expires_in}` |
| `auth.poll` | 宿主 → 插件 | `{key}` → `{state,credential,nickname,persist?}` |
| `auth.apply` | 宿主 → 插件 | `{credential}` → `null` |

`command.handle`、`callback.handle`、`inline.handle`、`message.match` 为 ABI 预留，v1 不调用。

### Manifest

```json
{
  "name": "example",
  "display_name": "示例音乐",
  "version": "1.0.0",
  "abi": 1,
  "code": "ex",
  "capabilities": ["provider", "recognizer", "auth"],
  "auth_required": false,
  "default_tag_format": "id3",
  "commands": [
    {"name": "example", "description": "示例 provider 点歌"},
    {"name": "program", "description": "按节目 ID 点歌", "type": "template",
     "template": "https://music.163.com/program?id={args}"}
  ],
  "resources": {"memory_mb": 128, "instances": 1},
  "http_allowed_hosts": ["example.com"]
}
```

- `name`：`^[a-z0-9][a-z0-9_-]{0,31}$`，用于 `[plugins] enabled/default` 与 `/search <name>`
- `code`：`^[a-z0-9]{1,8}$`，用于 Telegram 回调/start 参数（`code_base64url(id)`）
- `capabilities`：`provider` / `recognizer` / `auth`
- `commands`：`type` 为 `provider`（默认，参数交给该插件）或 `template`（`{args}` 替换后生成链接再解析）
- `http_allowed_hosts`：为空表示不限制；支持 `*.example.com` 通配
- `resources.instances`：覆盖全局实例池大小；`memory_mb` 目前作为文档提示

### Track / Ref

插件返回的 `ref`、`track`、`track summary` 字段与宿主 `internal/provider/types.go` 一致。
`ref.page_url` / `ref.share_text` 用于在没有额外 RPC 的情况下提供展示链接；
`track` 中的同名字段优先。`track.tag_format` 可选 `netease163key`、`id3` 或空。

## 宿主能力

| host name | 说明 |
| --- | --- |
| `log` | `{level,message}`，宿主加 `[plugin:<name>]` 前缀 |
| `http.request` | `{method,url,headers,body_b64,timeout_ms}` → `{status,headers,body_b64}` |
| `kv.get/set/delete/list` | 按插件隔离的 KV（SQLite），用于保存 cookie/token 等凭据 |
| `config.get` | 只返回该插件的 `[plugins.config.<name>]` |
| `time.now` / `random` | 便利接口 |

`pluginsdk.NewHTTPClient()` 返回一个把请求转发到 `http.request` 的 `*http.Client`，
因此依赖 `net/http` 的第三方库（如 `Music163Api-Go`）可以几乎原样运行在插件里。

HTTP 约定：

- 只允许 http/https；默认拒绝环回、私网、链路本地地址（宿主可配置）
- `http_allowed_hosts` 非空时只允许列出的 host
- 不自动跟随重定向（`CheckRedirect = ErrUseLastResponse`），由插件自己的 client 处理
- 4xx/5xx 作为正常响应返回，传输错误才返回 host error
- 单次响应体有大小上限（`[plugins] http_max_bytes`）

## 开发建议

1. 从 `plugins/example/` 复制一份，改 `go.mod` 的 module 路径。
2. `handle` 里按 `c.Action()` 分发；`init` 中用 `c.Config(&cfg)` 读取配置。
3. 需要登录的插件用 `c.KVGet/KVSet` 保存凭据，并在 `auth.poll` 返回
   `persist:{section,key}` 让宿主写回配置文件（网易云插件写回 `[netease] music_u`）。
4. 纯逻辑（链接解析、字段映射）写成不依赖 `Context` 的函数，在宿主上直接 `go test`；
   `//go:wasmexport` / `//go:wasmimport` 只在 `//go:build wasip1` 文件中，非 wasm 构建
   会使用 SDK 的 stub，因此插件 module 可以正常跑原生单测。
5. 不要在 `provider.match` 里做重网络请求（短链跳转除外），它会在每条消息上调用。
6. 插件 panic 会被 SDK 转为 `internal` 错误；超时、trap 或 OOM 时宿主会丢弃并重建实例。

## 扩展其它平台

Spotify / QQ 音乐等 provider 不需要改动宿主：

1. 复制 `plugins/example/`，实现 `provider.match`（识别链接/ID）、`provider.resolve`
   （元数据 + 可下载地址）、`provider.search`、`provider.lyrics`；
2. 用 `[plugins] enabled` 启用，并用 `/search <name> <关键词>` 验证；
3. 需要登录时实现 `auth.*`，凭据用 `c.KVGet/KVSet` 保存；
4. 若平台返回加密/签名的音频地址，插件负责在 `resolve` 时换算出宿主可直接下载的 URL；
   宿主会在下载前再次校验 scheme 与私网地址。

宿主不内置任何平台实现，默认 provider 由 `[plugins] default` 决定。

## 安全边界

插件是半可信代码：运行在 wazero 沙箱中，只能通过宿主导出的能力访问外部世界。
宿主会限制 HTTP 目标、响应大小、内存与调用时长。插件返回的
`audio_url` / `cover_url` 在下载前也会做 scheme 与私网检查（可通过
`[plugins] allow_private_networks` 放开，仅供本地测试）。
