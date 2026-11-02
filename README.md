<h1 align="center">TuneweaveBot</h1>

<h4 align="center">可扩展的音乐 Telegram Bot —— 点歌、搜索、歌词、识曲，音乐平台由 WASM 插件提供</h4>

<p align="center">
	<a href="https://github.com/XiaoMengXinX/TuneweaveBot/releases">
		<img src="https://img.shields.io/github/v/release/XiaoMengXinX/TuneweaveBot?include_prereleases&style=flat-square">
	</a>
	<a href="https://github.com/XiaoMengXinX/TuneweaveBot/actions/workflows/build.yml">
		<img src="https://img.shields.io/github/actions/workflow/status/XiaoMengXinX/TuneweaveBot/build.yml?style=flat-square">
	</a>
</p>

宿主负责 Telegram 交互、缓存、下载上传与打标；链接解析、搜索、歌词、识曲、登录等平台能力全部由
沙箱化的 WASM 插件提供。内置网易云音乐插件，接入其他平台无需改动宿主。

## ✨ 特性

- **插件化**：每个平台一个 `.wasm` 插件，多个 provider 可并存，`/search <provider>` 指定来源
- **点歌**：分享链接嗅探（含短链）、搜索翻页、inline 模式，自动写入标签与封面，支持无损
- **识曲**：回复语音即可识别，纯 Go 实现，无需 FFmpeg
- **Guest Mode**：bot 无需进群，回复分享链接或语音即可点歌、识曲
- **登录**：管理员扫码或提交 token，凭据自动保存，失效时提醒
- **运维**：`/reload` 等当前任务完成后重载；插件定时检查更新，一键更新，失败自动回滚

## 🚀 快速开始

### Docker

```sh
cp config_example.toml config.toml   # 填写 [bot] token 与 admin
mkdir -p data
docker compose up -d
```

镜像已内置插件。`data/` 保存数据库与登录凭据，请持久化挂载。`docker-compose.yml` 附带自建
Bot API 服务：使用时填写其中的 `TELEGRAM_API_ID` / `TELEGRAM_API_HASH`，并把 `[bot] api_url`
设为 `http://bot-api:8081`；不需要可删除该服务。

### 预编译版本

从 [Releases](https://github.com/XiaoMengXinX/TuneweaveBot/releases) 下载主程序，从
[plugin release](https://github.com/XiaoMengXinX/TuneweaveBot/releases/tag/plugin) 下载
`netease.wasm.gz` 放进 `plugins/`，然后：

```sh
./TuneweaveBot -c config.toml
```

### 从源码构建

需要 Go 1.26+。

```sh
git clone https://github.com/XiaoMengXinX/TuneweaveBot && cd TuneweaveBot
bash build.sh                            # 主程序 ./TuneweaveBot，插件 dist/plugins/*.wasm.gz
GOOS=windows GOARCH=amd64 bash build.sh  # 交叉编译
```

把 `dist/plugins/` 中的插件放进 `[plugins] dir`（默认 `./plugins`），或把 `dir` 指向
`dist/plugins`。插件目录为空或缺少默认 provider 时 bot 拒绝启动。

## ⚙️ 配置

复制 [`config_example.toml`](config_example.toml) 为 `config.toml`，最少只需：

```toml
[bot]
token = "YOUR_BOT_TOKEN"
admin = [123456]           # 管理员用户 ID

[plugins.netease]          # 每个插件一个配置段，原样传给插件
music_u = ""               # 可留空，之后用 /login 登录；下载无损需 VIP
level = "hires"            # 优先音质，无权限时自动降级
```

- 完整配置项及说明见 `config_example.toml`，或用 `./TuneweaveBot gen-config` 按已装插件生成
- 环境变量可覆盖任意配置：`TUNEWEAVE_BOT_TOKEN`、`TUNEWEAVE_DOWNLOAD_TIMEOUT`；插件配置用
  `TUNEWEAVE_PLUGINS_CONFIG_<插件名>_<键>`，如 `TUNEWEAVE_PLUGINS_CONFIG_NETEASE_LEVEL=lossless`
- 启动时若缺少插件声明的配置项会自动补全（终端运行时先询问）
- 指令菜单启动时自动注册；inline 模式需在 @BotFather 手动开启

## 🤖 命令

### 用户

| 命令 | 说明 |
| --- | --- |
| `/music <ID/关键词>` | 用默认 provider 点歌；直接发送分享链接也可 |
| `/search [provider] <关键词>` | 搜索并翻页（最多 5 页），按钮仅搜索者 10 分钟内可用 |
| `/lyric <ID/关键词>` | 发送歌词文件；也可回复含分享链接的消息 |
| `/recognize` | 回复一条语音，识别歌曲 |
| `/status` · `/about` | 缓存与插件状态 · 关于 |
| `/netease` · `/program` | 网易云插件声明的命令，按 ID 或关键词点歌 · 按节目 ID 点歌 |

`provider` 可写插件名或短 code（如 `netease` / `ne`）。inline 模式输入 `help` 查看用法。

### 管理员

| 命令 | 说明 |
| --- | --- |
| `/login [provider] [token]` | 扫码登录，或带 `token` 后回复消息提交 token/cookie |
| `/reload` | 等当前任务完成后重载配置与插件 |
| `/checkupdate` | 检查插件更新 |
| `/rmcache <ID/链接>` | 清除歌曲缓存 |
| `/setcachechat [群 ID]` | 设置 Guest Mode 缓存群，`0` 关闭 |

## 📖 说明

**登录**：bot 启动不依赖登录状态。某个 provider 凭据缺失或失效时暂停其新歌下载（已缓存歌曲照常
发送），每 10 分钟提醒管理员；`/login` 成功后凭据写回配置并自动恢复。

**插件更新**：发布版插件按 `[plugins] update_cron`（默认每天 4:00）检查更新，远端版本号更大或
版本相同但构建更新时，向管理员发送「立即更新」按钮（24 小时内有效，仅可用一次）。更新前校验
sha256 与版本信息并备份旧文件，新版本加载失败时自动回滚。本地构建的插件不参与自动更新。

**Guest Mode**：在 @BotFather 开启后，在任意聊天回复分享链接或语音并 `@你的Bot`，或直接
`@你的Bot 分享链接`。已缓存的歌曲直接发送，未缓存的返回歌曲信息与「在 Bot 中获取」按钮。
用 `/setcachechat` 设置缓存群后，未缓存的歌曲会先上传到缓存群再发给访客。自建 Bot API 需支持
Bot API 10.0 的 `guest_message` 与 `answerGuestQuery`。

## 🧩 插件开发

插件是独立的 Go module，编译为 `.wasm`（可 gzip 为 `.wasm.gz`）放入插件目录即可。

- `pluginsdk/`：插件 SDK
- `plugins/netease/`：官方网易云插件（provider、识曲、登录）
- `plugins/example/`：最小示例

ABI、action 列表与开发步骤见 [`plugins/README.md`](plugins/README.md)。

## 📄 许可

见 [LICENSE](LICENSE)。听歌识曲基于
[NeteaseCloudMusic-Audio-Recognize-Reverse](https://github.com/XiaoMengXinX/NeteaseCloudMusic-Audio-Recognize-Reverse)。
