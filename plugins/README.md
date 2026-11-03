# 插件开发指南

TuneweaveBot 的核心原则是**宿主对具体平台零知识**：所有与音乐平台相关的逻辑
（链接解析、元数据、搜索、歌词、识曲、登录）都在 WASM 插件里，宿主
（`internal/plugin`）只提供运行时与通用能力，不认识任何一个具体平台。

宿主负责：加载 `.wasm` / `.wasm.gz`、维护实例池、施加超时/内存上限、转发 HTTP（带代理与
SSRF 防护）、提供按插件隔离的 KV、把插件能力包装成
`provider.Provider` / `provider.Recognizer` / `provider.Authenticator`。

插件是独立的 Go module，只要通过 `pluginsdk` 注册一个 `handle` 函数并在 manifest
里声明能力即可，无需了解宿主内部。

---

## 1. 快速开始

```sh
cp -r plugins/example plugins/foo        # 复制最小示例
```

1. 改 `plugins/foo/go.mod` 的 module 路径。
2. 在 manifest 里把 `name`/`display_name`/`code` 换成你的平台。
3. 实现 `provider.match` / `provider.resolve`（最小可用的点歌只需这两个）。
4. 构建并验证：

```sh
NAME=foo bash build-plugins.sh && cp dist/plugins/foo.wasm.gz plugins/
# config.toml 里 [plugins] enabled = ["foo"]，然后 /music foo <ID>
# （match + resolve 即可点歌；实现 provider.search 后可用 /search foo <关键词>）
```

`go.work` 已包含 `.`、`./pluginsdk`、`./plugins/netease`、`./plugins/example`。
新插件的 `go.mod` 里加：

```go
require github.com/XiaoMengXinX/TuneweaveBot/pluginsdk v0.0.0

replace github.com/XiaoMengXinX/TuneweaveBot/pluginsdk => ../../pluginsdk
```

并把目录加进 `go.work`（`go work use ./plugins/foo`）。

---

## 2. 目录与构建

```text
pluginsdk/            插件 SDK（独立 module，封装 ABI 协议）
plugins/netease/      官方网易云插件（provider + recognizer + auth）
plugins/example/      最小 provider 示例
dist/plugins/         构建产物（不进版本库）
plugins/*.wasm[.gz]   运行时加载目录（不进版本库）
```

```sh
bash build-plugins.sh              # 构建 netease 到 dist/plugins/netease.wasm.gz
NAME=example bash build-plugins.sh # 构建其他插件
```

产物用 gzip 压缩（约为原来的 1/4），宿主加载时解压，未压缩的 `.wasm` 同样可以直接加载；
同一插件只能保留一种格式。脚本同时生成 `dist/plugins/version.json`（版本取自插件 `main.go`
的 `pluginVersion`，sha256 按压缩文件计算），并把 commit 与构建时间注入 manifest。

底层命令：

```sh
CGO_ENABLED=0 GOOS=wasip1 GOARCH=wasm \
  go build -buildmode=c-shared -trimpath -ldflags="-s -w" \
  -o dist/plugins/<name>.wasm ./plugins/<name>
```

文件名（去掉 `.wasm` / `.wasm.gz`）用于匹配 `[plugins] enabled`。插件通过 manifest 的 `name`
注册为 provider，两者通常保持一致。默认 provider 缺失或插件目录为空时宿主拒绝启动。

---

## 3. 运行模型

每个插件有一个常驻实例池，大小取 `[plugins.<name>] instances`（未设置时用
`[plugins] instances`，默认 2），再以 manifest 的 `resources.instances` 为上限，最多 16。

`provider.resolve` 与 `recognizer.recognize` 是重任务，不占用常驻池：每次调用都新建一个
临时实例（执行 `init` 后调用），结束即销毁。全局同时进行的重任务数受 `[download]
concurrency` 限制。这样下载再多也不会挡住搜索、翻页等轻量调用，重任务的峰值内存也随
实例销毁归还。宿主对实例做如下保证：

- **并发**：同一池内的实例并行处理不同调用；池满时调用方排队。
- **自愈**：实例 panic、trap、超时或 OOM 时，宿主丢弃该实例并重建，重建时会重新
  执行 `init`（并重放最近一次登录凭据，见 §6）。
- **隔离**：KV 与 `config.get` 都按插件名隔离，插件之间互不可见。

因此插件代码应当是**无状态可重入**的：把会话状态放进 KV，而不是包级变量——包级变量
在实例重建后会丢失。

---

## 4. ABI v1

### 导出（插件 → 宿主）

| 导出 | 说明 |
| --- | --- |
| `_initialize` | Go `c-shared` reactor 自带，宿主实例化时调用 |
| `alloc(size u32) -> u32` | 分配并持有内存（SDK 用 map 固定，避免 GC 移动） |
| `dealloc(ptr u32, size u32)` | 释放 `alloc`/`handle` 返回的内存 |
| `handle(ptr u32, len u32) -> u64` | 处理一次调用，返回 `ptr \| len<<32` |

Go 的 reactor 模块不会执行 `main()`，插件必须在 `init()` 里注册入口：

```go
func init() { pluginsdk.Main(handle) }
func main() {}
```

### 导入（宿主 → 插件）

宿主导出模块 `host` 的唯一函数：

```text
host.call(namePtr, nameLen, reqPtr, reqLen, respPtr, respCap) -> i64
```

- `>= 0`：写入 `respPtr` 的字节数
- `-2`：缓冲区不足；宿主在 `respPtr` 前 8 字节写入所需大小，插件扩容后重试
- `-1`：其他错误

host 函数**不回调插件的 `alloc`**，响应缓冲区由插件预先分配并传入。整套协议由
`pluginsdk` 处理，插件作者只需调用 `Context` 方法。

### 信封

请求与响应都是 UTF-8 JSON，二进制字段用 base64：

```json
{"abi":1,"action":"provider.resolve","payload":{"ref":{"id":"123"}}}
{"ok":true,"data":{"track":{"title":"..."}}}
{"ok":false,"error":{"code":"not_found","message":"song 123"}}
```

错误码：`no_result`、`unsupported`、`auth_required`、`rate_limited`、`not_found`、
`invalid_request`、`internal`。宿主据此映射用户文案，`message` 只写日志不直接展示。
用 `pluginsdk.Fail(code, message)` 构造。

---

## 5. Actions

| Action | Payload → Data | 能力 |
| --- | --- | --- |
| `manifest` | `{}` → manifest | 必需 |
| `init` | `{config, credential}` → `null` | 必需 |
| `shutdown` | `{}` → `null` | 必需 |
| `provider.match` | `{text}` → `{ref}` 或 `{ref:null}` | provider |
| `provider.canonicalize` | `{ref}` → `{ref}` | provider |
| `provider.resolve` | `{ref}` → `{track}` | provider |
| `provider.metadata` | `{ref}` → `{track: {ref,title,artists,album,duration_ms}}` | provider（可选） |
| `provider.search` | `{keyword,limit,offset}` → `{items,total}` | provider |
| `provider.lyrics` | `{ref}` → `{lrc,title,artists,album}` | provider |
| `recognizer.recognize` | `{audio(b64),mime}` → `{ref}` | recognizer |
| `auth.status` | `{}` → `{authenticated,account}` | auth |
| `auth.begin` | `{mode}` → `{type,content,key,expires_in}` | auth |
| `auth.poll` | `{key}` → `{state,credential,nickname,persist?}` | auth |
| `auth.submit` | `{key,token}` → `{state,credential,nickname,persist?}` | auth（token 登录） |
| `auth.apply` | `{credential}` → `null` | auth |

能力相互独立：只做点歌实现 `provider.*`；识曲另加 `recognizer.recognize`；登录另加
`auth.*`；需要在插件内解密音频再交给宿主的另加 `media`（见 §9）。只有**默认 provider**
必须具备 `provider` 能力。

- `provider.match`：**在每条消息上调用**，只做本地解析（识别链接/ID），不得做重网络
  请求（短链跳转除外）。无法识别时返回 `{ref:null}`。
- `provider.canonicalize`：把节目、电台等子资源归一成可缓存的歌曲 `ref`。
- `provider.resolve`：返回元数据与可下载音频；没有音频时同时返回 `no_audio: {code, fee, reason}`，
  `reason` 是给用户看的原因（如“无版权或已下架”），宿主原样展示，空时显示通用提示。
- `provider.metadata`：只返回展示用元数据（Guest Mode 等场景）。未实现时宿主回退到
  `provider.resolve`；resolve 代价高（如需下载解密）的插件应当实现它。

### Manifest

```json
{
  "name": "example",
  "display_name": "示例音乐",
  "version": "1.0.0",
  "commit": "9620876...",
  "build_time": "2026-11-03T01:40:00Z",
  "update_url": "https://github.com/<owner>/<repo>/releases/download/plugin/example.wasm.gz",
  "version_url": "https://github.com/<owner>/<repo>/releases/download/plugin/version.json",
  "abi": 1,
  "code": "ex",
  "capabilities": ["provider", "recognizer", "auth"],
  "auth_required": false,
  "default_tag_format": "id3",
  "commands": [
    {"name": "example", "description": "示例 provider 点歌"},
    {"name": "program", "description": "按节目 ID 点歌", "type": "template",
     "template": "https://example.com/program?id={args}"}
  ],
  "resources": {"memory_mb": 128, "instances": 1},
  "http_allowed_hosts": ["example.com"],
  "link_hosts": ["example.com"],
  "config_schema": [
    {"key": "token", "type": "string", "default": "", "secret": true,
     "comment": "登录凭据，可用 /login 获取"}
  ]
}
```

- `name`：`^[a-z0-9][a-z0-9_-]{0,31}$`，用于 `[plugins] enabled/default` 与 `/search <name>`
- `code`：`^[a-z0-9]{1,8}$`，用于 Telegram 回调/start 参数（`code_base64url(id)`）
- `commit` / `build_time`：可选，构建所用的 git commit 与构建时间（RFC 3339），显示在
  `/status`。通常用 `-ldflags "-X main.commitSHA=... -X main.buildTime=..."` 在构建时注入，
  `build-plugins.sh` 已自动注入
- `update_url` / `version_url`：可选，须同时声明。最新 wasm 与 `version.json`
  （`{"version", "build_time", "commit", "sha256"}`）的下载地址，宿主据此检查和应用更新：
  远端版本号更大时提示，版本号相同或无法按数字比较时比较构建时间；替换前校验 sha256，以及新 wasm 的 name、version、
  build_time 与 `version.json` 一致。更新沿用原文件格式（`.wasm` 或 `.wasm.gz`），旧文件备份为同名 `.bak`，新版本加载失败时自动回滚。
  `build-plugins.sh` 在设置 `RELEASE_URL` 时注入（`$RELEASE_URL/<name>.wasm.gz` 与
  `$RELEASE_URL/version.json`），发布工作流会传入本仓库 `plugin` release 的地址；本地构建不注入，
  因而不参与自动更新
- `commands.type`：`provider`（默认，参数交给该插件）或 `template`（`{args}` 替换后生成链接再解析）
- `http_allowed_hosts`：为空表示禁止一切 HTTP；支持 `*.example.com` 通配
- `link_hosts`：`provider.match` 能识别的链接域名（子串匹配）。声明后，像链接的文本只有
  包含其中某个域名时才会调用该插件的 `provider.match`；纯 ID 等不像链接的文本不受影响。
  不声明则每条消息都会调用
- `resources.instances`：插件支持的最大实例数，运维配置超过它时按它截断；`memory_mb`
  目前仅作文档提示

### config_schema

声明该插件 `[plugins.<name>]` 段支持的配置项，宿主用它：

- `./TuneweaveBot gen-config` 生成带注释的配置模板；
- 启动时检测配置缺失的字段并补全（终端会询问，非终端自动补全）。

字段：`key`（`^[a-z0-9][a-z0-9_]*$`）、`type`（`string`/`int`/`bool`）、
`default`（可选，类型须与 `type` 一致）、`comment`（可选注释）、`secret`
（可选，true 时模板只写键名+空值，不写默认值）。宿主不校验配置项的业务语义，
那是插件在 `init` 里的事（例如音质枚举、降级逻辑）。

### Track / Ref

插件返回的 `ref`、`track`、`track summary` 字段与宿主 `internal/provider/types.go`
一致。`ref.page_url` / `ref.share_text` 在没有额外 RPC 时提供展示链接，`track` 中的
同名字段优先。`track.bit_rate` 单位为 bps。

`track.tag_format` 可选 `netease163key`、`id3` 或空（不写标签）。`id3` 是通用格式，宿主按
文件类型写成 ID3（mp3）、Vorbis comment（flac）或 iTunes ilst（m4a/mp4），封面由宿主
下载并嵌入。`track.tag_payload` 结构：

```json
{
  "title": "...", "artists": ["..."], "album": "...",
  "album_artists": ["..."], "date": "2024-05-01",
  "track_number": 3, "track_total": 12, "disc_number": 1, "disc_total": 1,
  "composers": ["..."], "copyright": "℗ 2024 ...",
  "custom": {"ISRC": "...", "label": "..."}
}
```

除 `title`/`artists`/`album` 外均可省略；`custom` 写成 TXXX、同名 Vorbis 字段或
`----:com.apple.iTunes:<key>`。

`command.handle`、`callback.handle`、`inline.handle`、`message.match` 为 ABI 预留，v1 不调用。

---

## 6. 登录（auth）

宿主把登录抽象成多态流程，由 `auth.begin` 的返回类型决定交互方式：

| `type` | 交互 | 需要实现 |
| --- | --- | --- |
| `qr` | 宿主把 `content` 渲染成二维码，轮询 `auth.poll` | `begin`+`poll` |
| `link` | 宿主把 `content` 作为链接发出，轮询 `auth.poll` | `begin`+`poll` |
| `token` | 宿主提示用户回复消息发送 token，收到后调 `auth.submit` | `begin`+`submit` |

`/login <provider>` 用默认交互；`/login <provider> token` 请求 token 交互
（`auth.begin` 会收到 `{mode:"token"}`）。一个插件可同时支持多种方式：按 `mode`
在 `auth.begin` 里返回不同的 `type`。

- `auth.status` → `{authenticated, account}`：宿主启动时与每 30 分钟各查一次；返回
  未登录会暂停该 provider 的下载并每 2 小时提醒管理员，直到恢复。网络错误不算失效。
- `auth.poll`（qr/link）→ `{state}`：`waiting`/`scanned`/`success`/`expired`。
- `auth.submit`（token）→ 同 `auth.poll` 的结果结构；token 无效返回 `expired`。
- 成功时返回 `credential`（宿主广播给池内所有实例）、可选 `nickname`，以及可选
  `persist:{section,key}` 让宿主把凭据写回 `[plugins.<section>] <key>`。
- `auth.apply`：宿主把 `credential` 广播到每个实例时调用，你在这里更新内存状态并
  `KVSet` 持久化，使实例重建后仍能自动登录。

凭据读取优先级建议：登录持久化的 KV > 配置文件里的字段。参见
`plugins/netease/auth.go`。

---

## 7. 宿主能力

| host name | 说明 |
| --- | --- |
| `log` | `{level,message}`，level 为 `debug`/`info`/`warn`/`error`，宿主带上 `plugin=<name>` 字段 |
| `http.request` | `{method,url,headers,body_b64,timeout_ms}` → `{status,headers,body_b64}` |
| `kv.get/set/delete/list` | 按插件隔离的 KV（SQLite），保存 cookie/token 等 |
| `config.get` | 只返回该插件的 `[plugins.<name>]` |
| `time.now` / `random` | 便利接口（wasip1 下系统时间/熵不可直接用） |
| `media.open/write/close/abort` | 把插件生成的大音频流式落盘（需声明 `media` 能力），见 §9 |

`[log] level = "debug"` 时，宿主还会记录每次插件调用（action、常驻/临时实例、耗时、错误）
以及插件发出的每个 HTTP 请求和下载（方法、不含 query 的地址、状态码、大小、耗时）。插件自己的
debug 日志不要写入 cookie、token 等凭据内容。

`pluginsdk.NewHTTPClient()` 返回一个把请求转发到 `http.request` 的 `*http.Client`，
依赖 `net/http` 的第三方库（如 `Music163Api-Go`）几乎可以原样跑在插件里。

HTTP 约定：

- 只允许 http/https；默认拒绝环回、私网、链路本地地址（宿主可配置）
- 只允许 `http_allowed_hosts` 列出的 host
- 不自动跟随重定向（`CheckRedirect = ErrUseLastResponse`），由插件自己处理
- 4xx/5xx 作为正常响应返回，只有传输错误才是 host error
- 单次响应体有大小上限（`[plugins] http_max_bytes`，默认 8MB）
- 代理（`[plugins] proxy`）由宿主统一应用，插件无需关心

---

## 8. 开发建议

1. **纯逻辑单测**：链接解析、字段映射写成不依赖 `Context` 的函数，在宿主上直接
   `go test`。`//go:wasmexport` / `//go:wasmimport` 只在 `//go:build wasip1` 文件里，
   非 wasm 构建用 SDK 的 stub，因此插件 module 能跑原生单测。
2. **无状态**：会话态放 KV，不要放包级变量（实例重建会丢）。
3. **match 要轻**：`provider.match` 每条消息都调用，别做重网络请求。
4. **错误要分类**：用 `pluginsdk.Fail` 返回恰当的错误码，宿主据此给用户文案。
5. **panic 安全**：插件 panic 被 SDK 转成 `internal`；超时/trap/OOM 时实例被重建。

---

## 9. 数据面较大的平台（下载即解密）

有些平台的音频是加密流，需要先取密钥再解密。宿主对加密一无所知，
所以解密必须在插件内完成；但解密后的整轨可达几十 MB，用 `provider.resolve` 的
JSON 响应一次性穿过 ABI 既撞 `http_max_bytes` 又占实例内存。

正确切分用 **media 能力**：插件在 manifest 声明 `"media"`，自己完成解密，然后用
`Context.OpenMedia(ext)` 向宿主申请一个写入会话，分块 `sink.Write(chunk)` 把解密
字节流式交给宿主落盘，`sink.Close()` 结束；最后把 `track.AudioURL` 填成
`pluginsdk.MediaRef(sink.Token())`。宿主识别这个哨兵后直接使用已落盘的文件，跳过
URL 下载与校验。失败时 `sink.Abort()` 让宿主删除半成品。

密文本身也可以交给宿主下载：`Context.DownloadToScratch(url, opts)` 用宿主的多线程
下载器把文件落到沙箱内可写的 `/scratch`，并把进度（附 `opts.Title/Album`）实时显示在
状态消息里。该文件只在本次调用内有效，调用结束后宿主会删除。插件自己的临时文件也应
写在 `/scratch` 下。单个媒体文件（`media.write` 累计或 `media.download`）上限 2 GiB。

这样平台专属的取密钥/解密逻辑全在插件里，宿主只提供“把字节写到文件”这个中立原语，
依然不认识任何具体平台。解密应从 `/scratch` 文件按分片流式读写（如分片 MP4 逐个
moof+mdat 处理），实例内存只取决于单个分片大小，与音轨时长无关，默认
`memory_mb = 128` 即可。

解密往往还要一个密钥或设备文件。插件跑在沙箱里默认没有文件
系统访问，宿主实例化时只把 `[plugins] fs_dir` 这一个目录**只读**挂进沙箱。插件用
`os.ReadFile` 读相对 `fs_dir` 的路径即可；`fs_dir` 留空
则插件完全无文件系统访问。设备文件由 operator 放进 `fs_dir`，不随插件分发。

---

## 10. 安全边界

插件是半可信代码，运行在 wazero 沙箱中，只能通过宿主导出的能力访问外部世界。宿主
限制 HTTP 目标、响应大小、内存与调用时长。插件返回的 `audio_url` / `cover_url` 在
下载前也会做 scheme 与私网检查（可用 `[plugins] allow_private_networks` 放开，仅供
本地测试）。宿主不内置任何平台实现，默认 provider 由 `[plugins] default` 决定。

`[plugins] fs_dir` 把一个目录**只读**挂进所有插件的沙箱（默认不挂）。所有插件共享
一个可写的 `/scratch` 挂载，用于单次调用内的临时文件，宿主启动时会清空它。只读挂载仍可能被相对路径 `../../` 越界读取 `fs_dir` 之上的文件，所以只在
该目录放插件该看到的文件，不要指向家目录或仓库根。
