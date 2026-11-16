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
mkdir -p data plugins
curl -L -o plugins/netease.wasm.gz \
  https://github.com/XiaoMengXinX/TuneweaveBot/releases/download/plugin/netease.wasm.gz
docker compose up -d
```

镜像不含插件：插件放在挂载的 `plugins/` 中，`/checkupdate` 更新后的插件也写在这里。`data/`
保存数据库与登录凭据。两者都请持久化挂载。`docker-compose.yml` 附带自建
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

需要 Go 1.26+ 与 Node.js 22.12+（构建面板前端，产物嵌入二进制）。

```sh
git clone https://github.com/XiaoMengXinX/TuneweaveBot && cd TuneweaveBot
bash build.sh                            # 主程序 ./TuneweaveBot，插件 dist/plugins/*.wasm.gz
GOOS=windows GOARCH=amd64 bash build.sh  # 交叉编译
SKIP_WEB=1 bash build.sh                 # 跳过前端构建，沿用 internal/webapp/dist 的现有内容
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
- 环境变量可覆盖 `[bot]`、`[webapp]`、`[plugins]`、`[database]`、`[download]`、`[log]` 的配置项，格式为
  `TUNEWEAVE_<段>_<键>`，如 `TUNEWEAVE_BOT_TOKEN`、`TUNEWEAVE_DOWNLOAD_TIMEOUT`；`[plugins.<名称>]` 插件配置段不支持
- 启动时若缺少插件声明的配置项会自动补全（终端运行时先询问）
- 指令菜单启动时自动注册；inline 模式需在 @BotFather 手动开启

### 面板（Mini App）

面板是一个 Telegram Mini App：用户可以查看点过的歌曲、搜索点歌，`[bot] admin` 中的管理员还能查看
状态与统计、管理缓存、登录、更新插件、查看配置与日志。填写公网 HTTPS 地址后启用，留空则不启动 HTTP 服务：

```toml
[webapp]
url = "https://bot.example.com/"   # 面板的公网 HTTPS 地址
# listen = "127.0.0.1:8080"        # HTTP 服务的监听地址，默认只监听本机
```

启动时 bot 会把 Menu Button 注册为打开面板，私聊发送 `/start` 也会给出入口按钮，不需要在 @BotFather
中配置。之后移除 `url` 时，Menu Button 需在 @BotFather 中手动恢复。

bot 只在本机提供 HTTP，公网的 HTTPS 入口有两种部署模式，二选一：

| | 模式一：Tunnel 直连 | 模式二：Worker + Workers VPC |
| --- | --- | --- |
| 适用 | 有托管在 Cloudflare 上的域名 | 没有域名，使用免费的 `*.workers.dev` 地址 |
| 额外组件 | 无 | 一个 Worker，部署一次 |

两种模式下页面、接口、身份校验与限流都由 bot 提供，对外的访问范围相同，bot 的配置也相同，
`url` 填最终对外的地址。有域名时用模式一即可。

#### 模式一：Tunnel 直连

域名经 Cloudflare Tunnel（或其他反向代理）直接转发到 bot。在 tunnel 的配置中把域名指向监听地址：

```yaml
# ~/.cloudflared/config.yml
tunnel: <tunnel-id>
credentials-file: /path/to/<tunnel-id>.json
ingress:
  - hostname: bot.example.com
    service: http://127.0.0.1:8080
  - service: http_status:404
```

`url` 可以带路径前缀（如 `https://example.com/tuneweave/`），此时须以 `/` 结尾，转发时去掉该前缀。

#### 模式二：Worker + Workers VPC

一个 Cloudflare Worker 经 [Workers VPC](https://developers.cloudflare.com/workers-vpc/) 把全部请求转发给
bot，对外的地址是 Worker 的 `*.workers.dev` 域名，因此 tunnel 不需要绑定自己的域名。Worker 不保存页面，
只在边缘缓存带内容哈希的前端文件，部署一次即可，升级 bot 后不需要重新部署。

选用前需要知道：Workers VPC 目前处于 beta，功能和接口可能变化；面板的每个请求都计入 Worker 的请求额度
（免费计划每天 10 万次）。

1. 准备一个连接到 bot 所在机器的 Cloudflare Tunnel，不需要为它配置公网域名。
2. 安装 Worker 的依赖：

   ```sh
   cd worker && npm install
   ```

3. 把 bot 的面板服务注册为 VPC Service，记下输出的 Service ID。主机与端口是从 tunnel 所在机器访问
   bot 的地址，与 `[webapp] listen` 对应：

   ```sh
   npx wrangler vpc service create tuneweave-bot \
     --type http --tunnel-id <tunnel-id> --hostname localhost --http-port 8080
   ```

4. 复制 `wrangler.example.jsonc` 为 `wrangler.jsonc`，把 `service_id` 换成上一步的 ID，然后部署：

   ```sh
   cp wrangler.example.jsonc wrangler.jsonc
   npx wrangler deploy
   ```

5. 把 `[webapp] url` 设为 Worker 的地址（`https://<name>.<account>.workers.dev/`，或在 Cloudflare 中
   为 Worker 绑定的自定义域名），重启或 `/reload` bot。

bot 重载或未启动时，Worker 返回 502，已经打开的面板会提示稍后重试。

#### 共同事项

- 保持 `listen` 只监听本机。监听其他地址时，本机以外可以绕过 Cloudflare 直接访问，bot 启动时会给出警告。
  Docker 部署时让 cloudflared 与 bot 处于同一网络，把 `listen` 设为 `0.0.0.0:8080`，不要把端口映射到宿主机。
- 接口用 Mini App 的 initData（以 bot token 签名，24 小时内有效）校验身份，管理接口在服务端校验管理员。
- 限流按客户端 IP 和用户分别计数，超出后返回 429；搜索另有更严格的频率与并发限制，同一用户同时只处理
  一个点歌任务并沿用搜索按钮的冷却时间。客户端 IP 取自 `CF-Connecting-IP`、`X-Real-IP`（Worker 模式）或
  `X-Forwarded-For`，仅在连接来自本机或内网时采信。
- 面板中的点歌统计来自点歌记录表，从升级到带面板的版本后开始累计；inline 选歌不计入。

前端源码在 `web/`，`npm run dev:mock` 可用假数据在浏览器中预览界面；Worker 源码在 `worker/`。

### 从 Music163bot 迁移

启动时会检查数据库结构，不符合时拒绝启动并说明原因，不会改动该数据库。Music163bot 的数据库
（Go / v2 / Next）需要导入到新库：

```sh
./TuneweaveBot import-db -from /path/to/Music163bot/cache.db   # 导入到 [database] path
# Docker：把旧库放进 data/ 后执行
docker compose run --rm bot ./TuneweaveBot import-db -from /app/data/old.db
```

旧库只读不改；已删除或没有 file_id 的记录跳过，已有的歌曲不覆盖，可重复执行。也可用来合并另一个
TuneweaveBot 数据库。缓存的 file_id 只对上传它的 bot 有效，请继续使用原来的 bot token。

## 🤖 命令

### 用户

| 命令 | 说明 |
| --- | --- |
| `/music [provider] <ID/关键词>` | 点歌，如 `/music netease 晴天`；直接发送分享链接也可 |
| `/search [provider] <关键词>` | 搜索并翻页（最多 5 页），按钮仅搜索者 10 分钟内可用 |
| `/lyric [provider] <ID/关键词>` | 发送歌词文件；也可回复含分享链接的消息 |
| `/recognize` | 听歌识曲：回复一段含音乐的语音，识别是哪首歌 |
| `/help` | 列出全部命令、插件命令与可用来源 |
| `/status` · `/about` | 缓存与插件状态 · 版本与已装插件 |
| `/netease` · `/program` | 网易云插件声明的命令，按 ID 或关键词点歌 · 按节目 ID 点歌 |

`provider` 可写插件名或短 code（如 `netease` / `ne`），省略时用默认 provider。inline 模式输入 `help` 查看用法。

### 管理员

| 命令 | 说明 |
| --- | --- |
| `/login [provider] [token]` | 扫码登录，或带 `token` 后回复消息提交 token/cookie |
| `/reload` | 等当前任务完成后重载配置与插件 |
| `/checkupdate` | 检查插件更新 |
| `/plugin [插件名]` | 查看插件状态（版本、构建、登录、更新）；带插件名时显示 manifest |
| `/rmcache <ID/链接>` | 清除歌曲缓存 |
| `/setcachechat [群 ID]` | 设置 Guest Mode 缓存群，`0` 关闭 |

## 📖 说明

**登录**：bot 启动不依赖登录状态。某个 provider 凭据缺失或失效时暂停其新歌下载（已缓存歌曲照常
发送），每 2 小时提醒管理员；`/login` 成功后凭据写回配置并自动恢复。

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
