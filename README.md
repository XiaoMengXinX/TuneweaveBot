<h1 align="center">TuneweaveBot</h1>

<h4 align="center">可扩展的音乐 Telegram Bot —— 下载、分享、搜索、识曲，音乐平台由 WASM 插件提供</h4>

<p align="center">
	<a href="https://github.com/XiaoMengXinX/TuneweaveBot/releases">
		<img src="https://img.shields.io/github/v/release/XiaoMengXinX/TuneweaveBot?include_prereleases&style=flat-square">
	</a>
	<a href="https://github.com/XiaoMengXinX/TuneweaveBot/actions/workflows/build.yml">
		<img src="https://img.shields.io/github/actions/workflow/status/XiaoMengXinX/TuneweaveBot/build.yml?style=flat-square">
	</a>
</p>

TuneweaveBot 是一个 Telegram 点歌机器人。宿主只负责 Telegram 交互、缓存、下载、上传、
打标和 WASM 运行时；**具体音乐平台（链接解析、搜索、歌词、识曲、登录）全部由插件提供**。
内置网易云音乐插件，也可以自行编写插件接入其它平台，无需改动宿主代码。

## ✨ 特性

- **WASM 插件架构**：每个音乐平台是一个独立、沙箱化的 `.wasm` 插件，可增删替换
- **多 provider 并存**：`/search <provider> <关键词>` 指定来源，或在配置中设默认 provider
- 分享链接嗅探（网易云含 `163cn.tv` / `163cn.link` 短链）、inline 模式、歌曲搜索翻页
- 下载并发送音频，自动写入 ID3 / 网易云 163key 标签，支持无损 FLAC（需对应平台 VIP）
- 语音识别搜歌（`/recognize`，纯 Go 解析音频，无需 FFmpeg）
- 登录（管理员 `/login`，支持扫码与回复提交 token/cookie，免手动抓 cookie）
- Guest Mode：无需把 bot 拉进群，回复分享链接或语音即可点歌/识曲
- 优雅退出、`/reload` 进程内热重载、下载失败经反代重试

## ⚙️ 构建

需要 `Go 1.26` 或更高版本。

```sh
git clone https://github.com/XiaoMengXinX/TuneweaveBot
cd TuneweaveBot
bash build.sh

# 也可交叉编译：
GOOS=windows GOARCH=amd64 bash build.sh
```

`build.sh` 会同时构建主程序和 WASM 插件：主程序为 `TuneweaveBot`，插件产物在
`dist/plugins/*.wasm`，并复制一份到运行时的 `plugins/` 目录。也可单独构建插件：

```sh
bash build-plugins.sh              # 全部插件
PLUGINS="netease" bash build-plugins.sh
NO_COPY=1 bash build-plugins.sh    # 只输出到 dist/plugins
```

插件目录为空或默认 provider 缺失时 bot 会拒绝启动，请先构建插件。

## 🛠️ 部署

**配置文件**

复制 `config_example.toml` 为 `config.toml` 并按注释填写：

```toml
[bot]
token = "YOUR_BOT_TOKEN"   # 必填
admin = [123456]           # 管理员 ID
upload_timeout = "120s"    # 上传及其他 Telegram API 请求的总超时

[download]
timeout = "120s"
retries = 3
concurrency = 4
```

完整配置项见 `config_example.toml`。所有配置都可用 `TUNEWEAVE_` 前缀的环境变量覆盖
（如 `TUNEWEAVE_BOT_TOKEN`、`TUNEWEAVE_DOWNLOAD_TIMEOUT`），便于容器部署。

**插件配置**

每个插件对应一个独立的 `[plugins.<插件名>]` 段，整段原样传给该插件：

```toml
[plugins]
dir = "./plugins"          # .wasm 插件目录
enabled = ["netease"]      # 空 = 加载目录下全部插件
default = "netease"        # 默认 provider
instances = 1              # 每个插件的实例池大小
call_timeout = "30s"       # 单次插件调用超时
memory_mb = 256            # 单实例内存上限（数据面重的插件如 Spotify 解密需要更高）
http_max_bytes = 16777216  # 插件单次 HTTP 响应上限
allow_private_networks = false  # 插件 HTTP 与下载是否允许私网地址（默认拒绝，防 SSRF）
proxy = ""                 # 所有插件 HTTP 及宿主下载共用的代理，如 http://127.0.0.1:8080 或 socks5://127.0.0.1:1080
fs_dir = ""                # 只读挂进沙箱的宿主目录，插件按相对路径读文件（如 Spotify 的 .wvd）；留空=无文件系统访问

[plugins.netease]          # 原样传给 netease 插件，宿主不解释其内容
music_u = "YOUR_MUSIC_U"   # 网易云 cookie 中的 MUSIC_U（可留空，用 /login 登录；下载无损需 VIP）
level = "hires"            # 优先音质，账号无权限时自动降级
```

`[plugins.<插件名>]` 的字符串值可用环境变量
`TUNEWEAVE_PLUGINS_CONFIG_<插件名>_<键>` 覆盖，例如
`TUNEWEAVE_PLUGINS_CONFIG_NETEASE_LEVEL=lossless`。`./TuneweaveBot gen-config`
可按已装插件生成带注释的配置模板；启动时若配置缺少插件声明的字段会自动补全
（终端会先询问）。

插件把登录凭据存入数据库（`cache.db` 的 KV 表），并通过 `persist` 写回配置文件
对应字段，重启后自动复用。

指令菜单每次启动自动注册（无需在 BotFather 手动设置）：普通用户看到 `/music` `/search`
等公共指令，管理员额外看到 `/login` `/rmcache` `/reload` `/setcachechat`。inline 模式需在
BotFather 手动开启并设置占位文案。

**启动**

```
$ ./TuneweaveBot -c config.toml
2026/01/01 12:00:00 [INFO] TuneweaveBot v1.0.0 启动中 (main.go:53)
2026/01/01 12:00:20 [INFO] TuneweaveBot 验证成功 (app.go:100)
```

bot 启动不依赖任何平台的登录状态。某个 provider 凭据缺失或失效时，该 provider 的
新歌下载会暂停（已缓存歌曲仍可发送），管理员每 10 分钟收到一次提醒，直到恢复。
管理员用 `/login [provider]` 扫码登录，或 `/login <provider> token` 回复消息提交
token/cookie；成功后自动写回配置并恢复下载。管理员发送 `/reload` 可在进程内重载
配置，无需重启。

**Docker 部署**

```sh
mkdir -p data
docker compose up -d
```

`data/` 存放缓存数据库 `cache.db`（及 SQLite 的 `-wal`、`-shm` 文件）。首次启动自动创建。
请确保 `data/` 持久化挂载，否则重建容器会丢失缓存与登录凭据。
镜像以 root 运行，配置、数据库、缓存和日志均挂载到宿主机。

## 🧩 插件开发

宿主只保留 Telegram、缓存、下载、上传、打标和 WASM 运行时，音乐平台相关能力全部由插件提供。
插件是独立的 Go module，编译为 `.wasm` 后放入 `plugins/` 即可。

- SDK：`pluginsdk/`（`pluginsdk.Main`、HTTP/KV/Config 等宿主能力）
- 官方插件：`plugins/netease/`（网易云音乐，含识曲与登录）
- 最小示例：`plugins/example/`（固定数据的 provider，演示命令声明与 resolve/search/lyrics）

ABI、action 列表、内存约定和开发步骤见 [`plugins/README.md`](plugins/README.md)。
新增 provider 后用 `/search <插件名> <关键词>` 指定搜索，或在 `[plugins] default` 中设为默认。

## 🤖 命令

- `/music <ID/关键词>` —— 使用默认 provider 按 ID 或关键词点歌
- `/search [provider] <关键词>` —— 搜索歌曲，可翻页（最多 5 页），点数字按钮直接发送；
  按钮仅限搜索者在 10 分钟内使用。`provider` 可写插件名或短 code（如 `netease` / `ne`）
- `/netease <ID/关键词>`、`/program <ID>` —— 由网易云插件 manifest 声明的命令
- `/lyric <关键词/ID>`（或回复含分享链接、歌曲按钮的消息）—— 发送歌词文件，说明中附折叠歌词
- `/recognize`（回复一条语音）—— 语音识别搜歌
- `/status` —— 缓存与插件状态
- `/about` —— 关于本 bot
- `/start <ID>` —— 深链点歌（inline 未缓存时引导）
- 管理员：`/rmcache <ID/链接>` 清除缓存、`/reload` 重载配置、`/login [provider] [token]` 登录、
  `/setcachechat [群 ID]` 设置 Guest Mode 缓存群
- 直接发送分享链接即可嗅探点歌；inline 输入 `help` 查看用法

**Guest Mode**：在 @BotFather 为机器人开启 Guest Mode 后，可在任意群组或私聊里回复一条
分享链接或语音并发送 `@你的Bot用户名`，也可直接 `@你的Bot用户名 分享链接`。机器人无需加入该聊天：
已缓存的歌曲直接发送音频，未缓存的返回歌曲信息和「在 Bot 中获取」按钮；语音则返回识别结果和链接按钮。
每次访客调用只回复一条结果，不发送进度消息。

Guest Mode 无法直接上传文件。设置缓存群后，未缓存的歌曲会先下载并上传到缓存群，再以音频回复访客。
管理员在目标群内发送 `/setcachechat` 即设为当前群，或 `/setcachechat <群 ID>` 指定其它可发言的聊天，
`/setcachechat 0` 关闭；设置写入配置的 `[bot] cache_chat`。自建 Bot API 需支持 Bot API 10.0 的
`guest_message` 与 `answerGuestQuery`。

## 📄 许可

见 [LICENSE](LICENSE)。听歌识曲基于
[NeteaseCloudMusic-Audio-Recognize-Reverse](https://github.com/XiaoMengXinX/NeteaseCloudMusic-Audio-Recognize-Reverse)，
在本地提取音频指纹后请求网易云匹配，不依赖外部识曲服务或 FFmpeg。
