# 网易云音乐插件

TuneweaveBot 的官方网易云插件，实现 provider（点歌/搜索/歌词）、recognizer（识曲）、
auth（登录）三类能力。

## 构建

```sh
bash build-plugins.sh   # 输出 dist/plugins/netease.wasm 与 version.json
```

也可直接下载 [plugin](https://github.com/XiaoMengXinX/TuneweaveBot/releases/tag/plugin) release 中的预编译版本。

然后在 `config.toml` 的 `[plugins] enabled` 里加上 `netease`（留空则加载目录下全部）。

## 配置

```toml
[plugins.netease]
music_u = ""       # 网易云登录 Cookie 里的 MUSIC_U；留空可用 /login 登录
level = "hires"    # 优先音质，账号无权限时自动降级
```

- `music_u`：可留空，用 `/login netease` 扫码或 `/login netease token` 提交 cookie 登录；
  登录后持久化到 KV，优先级高于配置文件。
- `level`：`standard`/`higher`/`lossless`/`hires`/`jyeffect`（高清环绕声）/`sky`（沉浸环绕声）/
  `jymaster`（超清母带）。账号无权限的音质自动降级。

环境变量覆盖：`TUNEWEAVE_PLUGINS_CONFIG_NETEASE_LEVEL=lossless`。

## 登录

支持两种方式，由 `/login netease [token]` 选择：

- 不带参数：二维码扫码（宿主把 `content` 渲染成二维码，轮询直到扫码成功）。
- `token`：回复消息提交 MUSIC_U cookie。

两种方式成功后都把 MUSIC_U 写回 `[plugins.netease] music_u` 并存入 KV，重启自动复用。
`auth.status` 失效时宿主暂停下载并提醒管理员。

## 能力

- **点歌/搜索/歌词**：支持歌曲链接/ID、短链（163cn.tv/163cn.link）、节目（program）等。
- **识曲**：用户发送音频片段，返回匹配的歌曲。
- **标签**：`tag_format` 为 `netease163key`，宿主据此写入网易云的元数据标签。
