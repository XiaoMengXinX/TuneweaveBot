// Package bot 处理 Telegram 更新
package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/semaphore"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/auth"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/config"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/fetch"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/logging"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/plugin"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
)

// ErrReload /reload 触发的取消原因
var ErrReload = errors.New("reload requested")

// BuildInfo 编译信息，由 ldflags 注入
type BuildInfo struct {
	RuntimeVer  string
	VersionName string
	CommitSHA   string
	BuildTime   string
	BuildArch   string
}

type App struct {
	cfg           *config.Config
	cfgPath       string
	tg            *tgb.Bot
	me            *models.User
	store         *store.Store
	plugins       *plugin.Manager
	providers     *provider.Registry
	auth          *auth.Manager
	recognizers   []provider.Recognizer
	dl            *fetch.Downloader
	log           *logrus.Logger
	build         BuildInfo
	cacheDir      string
	http          *http.Client
	musicSem      *semaphore.Weighted
	statusSem     *semaphore.Weighted
	loginMu       sync.Mutex // 同时只允许一个登录会话
	loginCancelMu sync.Mutex // 保护 loginCancel 与 loginSurfaces
	loginCancel   context.CancelCauseFunc
	// loginSurfaces 记录当前会话的登录按钮消息，避免旧消息误取消新会话
	loginSurfaces map[surfaceKey]bool
	tokenMu       sync.Mutex                    // 保护 tokenWaiters
	tokenWaiters  map[surfaceKey]chan string    // token 登录：提示消息 -> 接收用户回复的通道
	cacheChat     atomic.Int64                  // 0 表示未设置缓存群
	warnMu        sync.Mutex                    // 保护 warnCancels
	warnCancels   map[string]context.CancelFunc // provider -> 告警循环取消函数
	usernames     []string                      // bot 的全部有效用户名，主用户名在前
	uploadClient  *http.Client                  // 流式上传音频用，超时同 [bot] upload_timeout
	reload        context.CancelCauseFunc       // 停止接收新消息并触发重载
	workCtx       context.Context               // handler 使用的 context，重载时不取消，进程退出时取消
	updateMu      sync.Mutex                    // 保护 updateOffers
	updateOffers  map[string]updateOffer        // 插件更新按钮 token -> 待应用的更新
	updating      atomic.Bool                   // 同时只进行一次插件更新
	cooldownMu    sync.Mutex                    // 保护 cooldowns
	cooldowns     map[cooldownKey]time.Time     // 冷却截止时间
	inflight      sync.WaitGroup                // 在途 handler，优雅退出时等待
}

// Run 阻塞运行 bot，返回 true 表示需重载配置
// Cookie 缺失或失效时引导扫码登录，超时则停止
func Run(ctx context.Context, cfg *config.Config, cfgPath string, log *logrus.Logger, build BuildInfo, client *http.Client) (reloadRequested bool, err error) {
	cacheDir := "./cache"
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return false, err
	}

	st, err := store.Open(cfg.Database.Path, logging.GormLogger(log, cfg.Log.Level))
	if err != nil {
		return false, err
	}
	defer st.Close()

	runCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	// 重载只停止轮询，在途 handler 继续使用 runCtx 直到完成
	pollCtx, stopPolling := context.WithCancelCause(runCtx)
	defer stopPolling(nil)

	log.AddHook(expectedErrorHook{})

	plugins, err := plugin.Load(runCtx, pluginConfig(cfg, cacheDir), st, log, client)
	if err != nil {
		return false, err
	}
	defer plugins.Close(context.Background())

	completeMissingConfig(cfg, cfgPath, plugins.ConfigSchemas(), log)

	a := &App{
		cfg:          cfg,
		cfgPath:      cfgPath,
		store:        st,
		plugins:      plugins,
		providers:    plugins.Providers(),
		auth:         plugins.Auths(),
		recognizers:  plugins.Recognizers(),
		dl:           fetch.New(cacheDir, cfg.Download.Timeout.D(), cfg.Download.Retries),
		log:          log,
		build:        build,
		cacheDir:     cacheDir,
		http:         &http.Client{Timeout: 60 * time.Second},
		musicSem:     semaphore.NewWeighted(int64(cfg.Download.Concurrency)),
		statusSem:    semaphore.NewWeighted(1),
		warnCancels:  map[string]context.CancelFunc{},
		tokenWaiters: map[surfaceKey]chan string{},
		uploadClient: &http.Client{Timeout: cfg.Bot.UploadTimeout.D()},
		reload:       stopPolling,
		workCtx:      runCtx,
		updateOffers: map[string]updateOffer{},
	}

	opts := []tgb.Option{
		tgb.WithServerURL(cfg.Bot.APIURL),
		tgb.WithHTTPClient(min(time.Minute, cfg.Bot.UploadTimeout.D()), &http.Client{Timeout: cfg.Bot.UploadTimeout.D()}),
		tgb.WithSkipGetMe(), // 统一在下方 GetMe 校验
		tgb.WithAllowedUpdates(tgb.AllowedUpdates{"message", "callback_query", "inline_query", "guest_message"}), // 避免沿用服务端设置而漏收回调

		tgb.WithMiddlewares(a.recoverMiddleware),
		tgb.WithDefaultHandler(a.handleDefault),
		tgb.WithErrorsHandler(func(err error) {
			// 停止或重载时中断的轮询请求不是错误
			if pollCtx.Err() == nil {
				log.Errorf("telegram: %v", err)
			}
		}),
	}
	if cfg.Bot.Debug {
		opts = append(opts, tgb.WithDebug())
		// 日志级别为 debug 时才写入日志文件，否则 SDK 默认只输出到终端
		if log.IsLevelEnabled(logrus.DebugLevel) {
			opts = append(opts, tgb.WithDebugHandler(func(format string, args ...any) {
				log.Debugf("tgbot: "+format, args...)
			}))
		}
	}
	tg, err := tgb.New(cfg.Bot.Token, opts...)
	if err != nil {
		return false, err
	}
	a.tg = tg
	a.cacheChat.Store(cfg.Bot.CacheChat)

	me, err := tg.GetMe(runCtx)
	if err != nil {
		return false, err
	}
	a.me = me
	a.usernames = botUsernames(runCtx, tg, me)
	log.Printf("%s 验证成功，用户名: @%s", me.Username, strings.Join(a.usernames, ", @"))

	a.registerHandlers(tg)
	a.setupCommands(runCtx, tg)
	a.checkCredentials(runCtx)
	a.notifyRollbacks(runCtx)
	go a.watchCookie(pollCtx)
	go a.watchPluginUpdates(pollCtx)
	tg.Start(pollCtx)

	// 等在途 handler 结束再由 defer 关闭 store 与插件，避免用到已关闭的资源。
	// 重载时让当前任务正常完成；进程退出时 handler 已随 runCtx 取消，只需短暂等待。
	reload := runCtx.Err() == nil && errors.Is(context.Cause(pollCtx), ErrReload)
	if reload {
		log.Infoln("已停止接收新消息，等待当前任务完成后重载")
		a.waitInflight(reloadDrainTimeout)
		return true, nil
	}
	a.waitInflight(30 * time.Second)
	return false, nil
}

// reloadDrainTimeout 是重载前等待在途任务的上限。
const reloadDrainTimeout = 10 * time.Minute

// requestReload 停止接收新消息，在途任务完成后重新加载配置与插件。
// 进行中的登录会话依赖新消息或长时间等待，直接结束。
func (a *App) requestReload() {
	a.loginCancelMu.Lock()
	if a.loginCancel != nil {
		a.loginCancel(ErrReload)
	}
	a.loginCancelMu.Unlock()
	a.reload(ErrReload)
}

// waitInflight 最多等待 timeout 让在途 handler 结束。
func (a *App) waitInflight(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		a.inflight.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		a.log.Warnln("等待在途请求超时，强制关闭")
	}
}

func (a *App) registerHandlers(tg *tgb.Bot) {
	registered := make(map[string]bool)
	register := func(name string, h tgb.HandlerFunc, m ...tgb.Middleware) {
		if registered[name] {
			return
		}
		registered[name] = true
		a.command(tg, name, h, m...)
	}
	register("start", a.handleStart)
	register("music", a.handleMusic)
	register("lyric", a.handleLyric)
	register("search", a.handleSearch)
	register("recognize", a.handleRecognize)
	register("about", a.handleAbout)
	register("status", a.handleStatus)
	register("help", a.handleHelp)
	register("rmcache", a.handleRmCache, a.adminOnly)
	register("reload", a.handleReload, a.adminOnly)
	register("checkupdate", a.handleCheckUpdate, a.adminOnly)
	register("plugin", a.handlePlugin, a.adminOnly)
	register("login", a.handleLogin, a.adminOnly)
	register("setcachechat", a.handleSetCacheChat, a.adminOnly)

	// 插件声明的命令（/netease、/program 等）由 manifest 决定，不写死在宿主
	if a.plugins != nil {
		for _, pc := range a.plugins.Commands() {
			cmd := pc.Command
			switch cmd.CommandType() {
			case "provider":
				register(cmd.Name, a.handleProviderMusic(pc.Plugin))
			case "template":
				register(cmd.Name, a.handleTemplateMusic(pc.Plugin, cmd.Template))
			}
		}
	}

	tg.RegisterHandler(tgb.HandlerTypeCallbackQueryData, "music ", tgb.MatchTypePrefix, a.handleCallbackMusic)
	tg.RegisterHandler(tgb.HandlerTypeCallbackQueryData, "search ", tgb.MatchTypePrefix, a.handleCallbackSearch)
	tg.RegisterHandler(tgb.HandlerTypeCallbackQueryData, "cancellogin", tgb.MatchTypeExact, a.handleCallbackCancelLogin, a.adminOnly)
	tg.RegisterHandler(tgb.HandlerTypeCallbackQueryData, updateCallbackPrefix, tgb.MatchTypePrefix, a.handleCallbackUpdate, a.adminOnly)
	tg.RegisterHandlerMatchFunc(func(u *models.Update) bool { return u.InlineQuery != nil }, a.handleInline)
	tg.RegisterHandlerMatchFunc(func(u *models.Update) bool { return u.GuestMessage != nil }, a.handleGuest)
}

func (a *App) command(tg *tgb.Bot, name string, h tgb.HandlerFunc, m ...tgb.Middleware) {
	tg.RegisterHandlerMatchFunc(matchCommand(name, a.usernames), h, m...)
}

// botUsernames 返回 bot 的全部有效用户名（主用户名在前）。getMe 只给出主用户名，
// 通过 Fragment 获得的其他用户名只在 getChat 的 active_usernames 中；获取失败时只用主用户名。
func botUsernames(ctx context.Context, tg *tgb.Bot, me *models.User) []string {
	names := []string{me.Username}
	chat, err := tg.GetChat(ctx, &tgb.GetChatParams{ChatID: me.ID})
	if err != nil {
		return names
	}
	for _, u := range chat.ActiveUsernames {
		if !slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(n, u) }) {
			names = append(names, u)
		}
	}
	return names
}

// matchCommand 匹配 /name 或 /name@用户名；用户名可以是 bot 的任一有效用户名，不区分大小写。
func matchCommand(name string, usernames []string) tgb.MatchFunc {
	return func(update *models.Update) bool {
		msg := update.Message
		if msg == nil || len(msg.Entities) == 0 {
			return false
		}
		e := msg.Entities[0]
		if e.Offset != 0 || e.Type != models.MessageEntityTypeBotCommand {
			return false
		}
		cmd := msg.Text[1:e.Length]
		if i := strings.IndexByte(cmd, '@'); i >= 0 {
			target := cmd[i+1:]
			return cmd[:i] == name && slices.ContainsFunc(usernames, func(u string) bool { return strings.EqualFold(u, target) })
		}
		return cmd == name
	}
}

// commandArguments 返回命令后的参数文本，只跳过命令与参数之间的一个分隔字符
func commandArguments(m *models.Message) string {
	e := m.Entities[0]
	if len(m.Text) == e.Length {
		return ""
	}
	return m.Text[e.Length+1:]
}

// pluginConfig 把 [plugins] 与各 [plugins.<name>] 段转换为插件加载配置。
func pluginConfig(cfg *config.Config, cacheDir string) plugin.Config {
	configs := make(map[string]json.RawMessage, len(cfg.Plugins.Config))
	for name, values := range cfg.Plugins.Config {
		if raw, err := json.Marshal(values); err == nil {
			configs[name] = raw
		}
	}
	return plugin.Config{
		Dir:     cfg.Plugins.Dir,
		Enabled: cfg.Plugins.Enabled,
		Default: cfg.Plugins.Default,
		Runtime: plugin.RuntimeConfig{
			CallTimeout:          cfg.Plugins.CallTimeout.D(),
			MemoryMB:             cfg.Plugins.MemoryMB,
			HTTPMaxBytes:         cfg.Plugins.HTTPMaxBytes,
			AllowPrivateNetworks: cfg.Plugins.AllowPrivateNetworks,
			Instances:            cfg.Plugins.Instances,
			MaxTasks:             cfg.Download.Concurrency,
			FSDir:                cfg.Plugins.FSDir,
			MediaDir:             filepath.Join(cacheDir, "plugin-media"),
			ScratchDir:           filepath.Join(cacheDir, "plugin-scratch"),
		},
		Configs:   configs,
		Instances: cfg.Plugins.PluginInstances,
	}
}

// checkCredentials 启动时校验各 auth provider 的凭据；缺失或失效的 provider
// 会被标记不可用并触发对管理员的重复告警。网络错误不视为失效。
func (a *App) checkCredentials(ctx context.Context) {
	for _, name := range a.auth.Names() {
		authenticator, ok := a.auth.Get(name)
		if !ok {
			continue
		}
		status, err := authenticator.Status(ctx)
		if err != nil {
			a.log.Warnf("无法验证 %s 登录状态: %v", name, err)
			continue
		}
		if !status.Authenticated {
			a.pauseDownloads(ctx, name)
		}
	}
}

// providerDisplay 把 provider 名解析为用户可读的显示名，未注册时回退到原名。
func (a *App) providerDisplay(name string) string {
	if p, ok := a.providers.Get(name); ok {
		return p.DisplayName()
	}
	return name
}
