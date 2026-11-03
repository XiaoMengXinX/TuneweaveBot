package bot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tgb "github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/fetch"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/plugin"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/render"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/store"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/tagging"
	"github.com/XiaoMengXinX/TuneweaveBot/internal/textx"
)

func (a *App) handleStart(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	m := update.Message
	if m.Chat.Type != models.ChatTypePrivate {
		return
	}
	ref, ok := a.parseStartParam(commandArguments(m))
	if !ok {
		return
	}
	if err := a.processMusic(ctx, ref, m, nil); err != nil {
		a.log.Errorln(err)
	}
}

// handleMusic 支持 /music [来源] <ID/关键词/链接>，未指定来源时用默认 provider。
func (a *App) handleMusic(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	m := update.Message
	providerName, args := a.parseProviderArgs(commandArguments(m))
	a.handleMusicArgs(ctx, m, providerName, args)
}

// handleProviderMusic 处理插件声明的 provider 命令（如 /netease），只在指定插件上匹配与搜索。
func (a *App) handleProviderMusic(name string) tgb.HandlerFunc {
	return func(ctx context.Context, tg *tgb.Bot, update *models.Update) {
		a.handleMusicArgs(ctx, update.Message, name, commandArguments(update.Message))
	}
}

// handleTemplateMusic 处理插件声明的模板命令（如 /program 2061）。
func (a *App) handleTemplateMusic(name, template string) tgb.HandlerFunc {
	return func(ctx context.Context, tg *tgb.Bot, update *models.Update) {
		m := update.Message
		args := strings.TrimSpace(commandArguments(m))
		if args == "" {
			if _, err := a.reply(ctx, m, render.InputIDorKeyword); err != nil {
				a.log.Errorln(err)
			}
			return
		}
		ref, ok := a.providers.MatchProvider(ctx, name, strings.ReplaceAll(template, "{args}", args))
		if !ok {
			if _, err := a.reply(ctx, m, render.NoResults); err != nil {
				a.log.Errorln(err)
			}
			return
		}
		if err := a.processMusic(ctx, *ref, m, nil); err != nil {
			a.log.Errorln(err)
		}
	}
}

// handleMusicArgs providerName 为空时使用默认 provider。
func (a *App) handleMusicArgs(ctx context.Context, m *models.Message, providerName, args string) {
	if args == "" {
		if _, err := a.reply(ctx, m, render.InputIDorKeyword); err != nil {
			a.log.Errorln(err)
		}
		return
	}
	if providerName == "" {
		if ref, _, ok := a.providers.Match(ctx, args); ok {
			if err := a.processMusic(ctx, *ref, m, nil); err != nil {
				a.log.Errorln(err)
			}
			return
		}
	} else if ref, ok := a.providers.MatchProvider(ctx, providerName, args); ok {
		if err := a.processMusic(ctx, *ref, m, nil); err != nil {
			a.log.Errorln(err)
		}
		return
	}
	items, _, err := a.providers.Search(ctx, providerName, args, 10, 0)
	if err != nil {
		a.log.Errorln(err)
	}
	if len(items) == 0 {
		if _, err := a.reply(ctx, m, render.NoResults); err != nil {
			a.log.Errorln(err)
		}
		return
	}
	if err := a.processMusic(ctx, items[0].Ref, m, nil); err != nil {
		a.log.Errorln(err)
	}
}

func (a *App) handleDefault(ctx context.Context, tg *tgb.Bot, update *models.Update) {
	m := update.Message
	if m == nil {
		return
	}
	if a.captureToken(ctx, m) {
		return
	}
	ref, ok := a.messageRef(ctx, m)
	if !ok {
		return
	}
	if err := a.processMusic(ctx, *ref, m, nil); err != nil {
		a.log.Errorln(err)
	}
}

// messageTexts 依次收集正文、说明、文字链接和按钮中的候选串
func messageTexts(m *models.Message) []string {
	links := []string{m.Text, m.Caption}
	for _, entities := range [][]models.MessageEntity{m.Entities, m.CaptionEntities} {
		for _, e := range entities {
			if e.Type == models.MessageEntityTypeTextLink {
				links = append(links, e.URL)
			}
		}
	}
	if m.ReplyMarkup != nil {
		for _, row := range m.ReplyMarkup.InlineKeyboard {
			for _, button := range row {
				if button.URL != "" {
					links = append(links, button.URL)
				}
			}
		}
	}
	return links
}

// messageRef 从消息中解析 provider 引用；普通文本不当作歌曲 ID，避免误触发
func (a *App) messageRef(ctx context.Context, m *models.Message) (*provider.Ref, bool) {
	for _, text := range messageTexts(m) {
		if !provider.LooksLikeLink(text) {
			continue
		}
		if ref, _, ok := a.providers.Match(ctx, text); ok {
			return ref, true
		}
	}
	return nil, false
}

// processMusic 以规范歌曲 Ref 为缓存键，ref.Sub 非空时先归一子资源。
func (a *App) processMusic(ctx context.Context, ref provider.Ref, message *models.Message, onInfo func(store.SongInfo)) error {
	chatID := message.Chat.ID
	reply := &models.ReplyParameters{MessageID: message.ID, AllowSendingWithoutReply: true}

	canonical := ref
	if ref.Sub != "" {
		resolved, err := a.providers.Canonicalize(ctx, ref)
		if err != nil {
			// 归一失败（如节目无主歌曲）须给用户反馈，否则发链接后毫无回应
			if _, rErr := a.reply(ctx, message, render.FetchInfoFailed); rErr != nil {
				a.log.Errorln(rErr)
			}
			return err
		}
		canonical = resolved
	}
	displayTrack := func() *provider.Track {
		if ref.Sub == "" {
			return nil
		}
		return &provider.Track{Ref: canonical, PageURL: a.refLink(ref), ShareText: a.providers.ShareURL(canonical)}
	}

	cached := func() (store.SongInfo, bool) {
		// FileID 为空的缓存行（手动插入或仅存了元数据）不算命中，否则会尝试 os.Open("")
		info, ok := a.store.GetByRef(canonical.Provider, canonical.ID)
		return info, ok && info.FileID != ""
	}

	if songInfo, ok := cached(); ok {
		if onInfo != nil {
			onInfo(songInfo)
		}
		msgResult, err := a.reply(ctx, message, render.MusicStatus(songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024, render.HitCache))
		if err != nil {
			return err
		}
		status := func(text string) error { return a.editMessage(ctx, chatID, msgResult.ID, text, nil) }
		return a.sendCached(ctx, songInfo, displayTrack(), chatID, reply, msgResult.ID, status)
	}

	// 凭据失效不影响缓存发送
	if !a.auth.Available(canonical.Provider) {
		if _, err := a.reply(ctx, message, render.CookieExpired(a.providerDisplay(canonical.Provider))); err != nil {
			a.log.Errorln(err)
		}
		return nil
	}

	msgResult, err := a.reply(ctx, message, render.WaitForDown)
	if err != nil {
		return err
	}
	status := func(text string) error { return a.editMessage(ctx, chatID, msgResult.ID, text, nil) }

	if err := a.musicSem.Acquire(ctx, 1); err != nil {
		return err
	}
	defer a.musicSem.Release(1)

	// 等待下载位时缓存可能已被其他任务填充
	if songInfo, ok := cached(); ok {
		if onInfo != nil {
			onInfo(songInfo)
		}
		if err := status(render.MusicStatus(songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024, render.HitCache)); err != nil {
			return err
		}
		return a.sendCached(ctx, songInfo, displayTrack(), chatID, reply, msgResult.ID, status)
	}

	if _, err := a.fetchMusic(ctx, canonical, ref, nil, message, onInfo, chatID, reply, status); err != nil {
		return err
	}
	return a.deleteMessage(ctx, chatID, msgResult.ID)
}

// sendCached 发送已缓存歌曲，成功后删除状态消息，失败时在状态消息上展示原因。
func (a *App) sendCached(ctx context.Context, songInfo store.SongInfo, track *provider.Track, chatID int64, reply *models.ReplyParameters, statusMsgID int, status func(string) error) error {
	if _, err := a.sendMusic(ctx, songInfo, track, "", "", chatID, reply); err != nil {
		a.showDownloadFailed(status, songInfo, render.SendFailed)
		return err
	}
	return a.deleteMessage(ctx, chatID, statusMsgID)
}

// fetchMusic 下载歌曲上传到 chatID 并写入缓存，失败原因已经由 status 展示
func (a *App) fetchMusic(ctx context.Context, ref, display provider.Ref, track *provider.Track, message *models.Message, onInfo func(store.SongInfo),
	chatID int64, reply *models.ReplyParameters, status func(string) error) (store.SongInfo, error) {
	var songInfo store.SongInfo
	if err := status(render.FetchInfo); err != nil {
		return songInfo, err
	}

	if track == nil {
		// 插件在 resolve 内经 media.download 下载时的进度，节流 5s
		var lastProgress int64
		resolveCtx := plugin.WithDownloadProgress(ctx, func(p plugin.DownloadProgress) {
			if p.Total <= 0 || time.Now().Unix()-lastProgress < 5 {
				return
			}
			lastProgress = time.Now().Unix()
			percent := p.Written * 100 / p.Total
			text := render.DownloadingWithInfo(p.Title, p.Album, p.Speed,
				float64(p.Written)/1024/1024, float64(p.Total)/1024/1024, percent)
			if err := status(text); err != nil {
				a.log.Errorln(err)
			}
		})
		resolved, err := a.providers.Resolve(resolveCtx, ref)
		if err != nil {
			if resolved != nil {
				a.fillSongInfo(&songInfo, resolved, ref, message)
				if onInfo != nil {
					onInfo(songInfo)
				}
			}
			var noAudio *provider.NoAudioError
			if errors.As(err, &noAudio) {
				a.showStatus(status, render.NoAudio(noAudio.Reason))
				return songInfo, fmt.Errorf("%s: %s %s, %v", render.NoAudioLog, ref.Provider, ref.ID, noAudio)
			}
			a.showStatus(status, render.FetchInfoFailed)
			return songInfo, err
		}
		if resolved == nil {
			// provider 返回 (nil, nil)：没有可用结果，否则后续解引用会 panic
			a.showStatus(status, render.FetchInfoFailed)
			return songInfo, fmt.Errorf("%s: music %s 返回空结果", render.FetchInfoFailed, ref.ID)
		}
		track = resolved
	}
	a.fillSongInfo(&songInfo, track, ref, message)
	if onInfo != nil {
		onInfo(songInfo)
	}
	// 节目页只用于本次展示，分享按钮仍指向规范歌曲
	if display.Sub != "" {
		track.PageURL = a.refLink(display)
	}
	if track.AudioURL == "" {
		a.showStatus(status, render.GetURLFailed)
		return songInfo, fmt.Errorf("%s: music %s", render.GetURLFailed, ref.ID)
	}
	allowPrivate := a.cfg.Plugins.AllowPrivateNetworks
	// 插件经 media.* 写入的文件：宿主直接取用，不走 URL 下载/校验。
	mediaPath := a.plugins.MediaPath(track.AudioURL)
	if mediaPath == "" {
		if err := fetch.ValidateURL(track.AudioURL, allowPrivate); err != nil {
			a.showStatus(status, render.DownloadFailed)
			return songInfo, err
		}
	}

	coverURL := track.CoverURL
	if coverURL != "" {
		if err := fetch.ValidateURL(coverURL, allowPrivate); err != nil {
			a.log.Errorln(err)
			coverURL = ""
		} else if picRes, err := a.http.Head(coverURL); err == nil {
			_ = picRes.Body.Close()
			songInfo.PicSize = int(picRes.ContentLength)
		} else {
			a.log.Errorln(err)
		}
	}

	timeStamp := time.Now().UnixMicro()
	var musicFileName string
	if mediaPath != "" {
		musicFileName = fmt.Sprintf("%d-%s", timeStamp, filepath.Base(mediaPath))
	} else {
		musicFileName = fmt.Sprintf("%d-%s", timeStamp, textx.URLBase(track.AudioURL))
	}
	musicPath := filepath.Join(a.cacheDir, musicFileName)

	var lastUpdateTime int64
	onProgress := func(p fetch.Progress) {
		if time.Now().Unix()-lastUpdateTime < 5 {
			return
		}
		statusText := render.Downloading
		if p.Attempt > 1 {
			statusText = render.Redownloading
		}
		if err := status(render.MusicStatus(songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024,
			statusText+render.DownloadStatus(p.Speed, float64(p.Written)/1024/1024, float64(p.Total)/1024/1024, p.Written*100/p.Total))); err != nil {
			a.log.Errorln(err)
		}
		lastUpdateTime = time.Now().Unix()
	}

	if mediaPath != "" {
		// 插件已在 resolve 内落盘，优先 rename 避免复制大文件
		if err := os.Rename(mediaPath, musicPath); err != nil {
			err := copyFile(mediaPath, musicPath)
			_ = os.Remove(mediaPath)
			if err != nil {
				a.showDownloadFailed(status, songInfo, render.DownloadFailed)
				return songInfo, err
			}
		}
		// media 路径下插件不提供 Size，以落盘文件为准
		if info, err := os.Stat(musicPath); err == nil {
			songInfo.MusicSize = int(info.Size())
		}
	} else {
		if err := status(render.MusicStatus(songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024, render.Downloading)); err != nil {
			return songInfo, err
		}
		if err := a.dl.DownloadMusic(ctx, fetch.Request{
			URL:      track.AudioURL,
			FileName: musicFileName,
			Policy:   track.Download,
		}, onProgress); err != nil {
			a.showDownloadFailed(status, songInfo, render.DownloadFailed)
			return songInfo, err
		}
	}

	// 音频下载成功后立即注册清理，确保后续任一步骤失败都不残留临时文件
	var picPath, resizePicPath, newDir string
	filePath := musicPath
	defer func() {
		for _, f := range []string{filePath, musicPath, resizePicPath, picPath} {
			if f == "" {
				continue
			}
			if err := os.Remove(f); err != nil && !os.IsNotExist(err) {
				a.log.Errorln(err)
			}
		}
		if newDir != "" {
			if err := os.RemoveAll(newDir); err != nil {
				a.log.Errorln(err)
			}
		}
	}()

	if track.MD5 != "" {
		if err := fetch.VerifyMD5(musicPath, track.MD5); err != nil {
			a.showDownloadFailed(status, songInfo, render.Md5VerFailed)
			return songInfo, err
		}
	}

	// 封面失败不阻断发歌
	if coverURL != "" {
		picName := fmt.Sprintf("%d-%s", timeStamp, textx.URLBase(coverURL))
		if err := a.dl.DownloadFile(coverURL, picName); err != nil {
			a.log.Errorln(err)
		} else {
			picPath = filepath.Join(a.cacheDir, picName)
			resized, err := tagging.ResizeCover(picPath)
			if err != nil {
				a.log.Errorln(err)
			} else {
				resizePicPath = resized
			}
		}
	}

	// 只有成功裁剪出的封面才嵌入，避免把损坏/过小的图片喂给标签库导致 panic
	var musicPic string
	if resizePicPath != "" {
		if picStat, err := os.Stat(picPath); err == nil && picStat.Size() > 2*1024*1024 {
			musicPic = resizePicPath
			if embStat, err := os.Stat(resizePicPath); err == nil {
				songInfo.EmbPicSize = int(embStat.Size())
			}
		} else {
			musicPic = picPath
			songInfo.EmbPicSize = songInfo.PicSize
		}
	}

	newDir = filepath.Join(a.cacheDir, strconv.FormatInt(timeStamp, 10))
	if err := os.Mkdir(newDir, os.ModePerm); err != nil {
		a.showDownloadFailed(status, songInfo, render.ProcessFailed)
		return songInfo, err
	}
	filePath = filepath.Join(newDir, songFileName(songInfo.SongArtists, songInfo.SongName, songInfo.FileExt))
	if err := os.Rename(musicPath, filePath); err != nil {
		a.log.Errorln(err)
		filePath = musicPath
	}

	if err := tagging.Write(filePath, musicPic, track.TagFormat, track.TagPayload); err != nil {
		a.showDownloadFailed(status, songInfo, render.ProcessFailed)
		return songInfo, err
	}

	if err := status(render.MusicStatus(songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024, render.Uploading)); err != nil {
		return songInfo, err
	}

	audio, err := a.sendMusic(ctx, songInfo, track, filePath, resizePicPath, chatID, reply)
	if err != nil {
		a.showDownloadFailed(status, songInfo, render.SendFailed)
		return songInfo, err
	}

	// Telegram 把非音频格式识别为 Document 时 audio.Audio 为 nil
	switch {
	case audio.Audio != nil:
		songInfo.FileID = audio.Audio.FileID
		if audio.Audio.Thumbnail != nil {
			songInfo.ThumbFileID = audio.Audio.Thumbnail.FileID
		}
	case audio.Document != nil:
		songInfo.FileID = audio.Document.FileID
	default:
		a.showDownloadFailed(status, songInfo, render.SendFailed)
		return songInfo, fmt.Errorf("发送后未返回文件 ID: music %s", ref.ID)
	}
	return songInfo, a.store.Save(&songInfo)
}

func (a *App) fillSongInfo(songInfo *store.SongInfo, track *provider.Track, ref provider.Ref, message *models.Message) {
	songInfo.FromChatID = message.Chat.ID
	if message.Chat.Type == models.ChatTypePrivate {
		songInfo.FromChatName = message.Chat.Username
	} else {
		songInfo.FromChatName = message.Chat.Title
	}
	if message.From != nil {
		songInfo.FromUserID = message.From.ID
		songInfo.FromUserName = message.From.Username
	}
	// provider 返回的规范 ID 优先；部分接口缺省 id 时回退到请求的 ref
	id := track.Ref.ID
	if id == "" || id == "0" {
		id = ref.ID
	}
	providerName := track.Ref.Provider
	if providerName == "" {
		providerName = ref.Provider
	}
	songInfo.Provider = providerName
	songInfo.TrackID = id
	songInfo.ProviderName = track.ProviderName
	// 缓存只保存规范歌曲页，节目页仅用于本次发送
	songInfo.PageURL = a.refLink(ref)
	if songInfo.PageURL == "" {
		songInfo.PageURL = track.PageURL
	}
	songInfo.ShareText = a.providers.ShareURL(ref)
	if songInfo.ShareText == "" {
		songInfo.ShareText = track.ShareText
	}
	songInfo.Duration = track.DurationMS / 1000
	songInfo.SongName = track.Title
	songInfo.SongArtists = textx.JoinNames(track.Artists)
	songInfo.SongAlbum = track.Album
	songInfo.FileExt = track.Ext
	if songInfo.FileExt == "" {
		songInfo.FileExt = "mp3"
	}
	songInfo.MusicSize = int(track.Size)
	songInfo.BitRate = track.BitRate
}

// songFileName 歌曲与歌词文件共用命名
func songFileName(artists, name, ext string) string {
	return textx.SanitizeFilename(fmt.Sprintf("%s - %s.%s", strings.ReplaceAll(artists, "/", ","), name, ext))
}

// copyFile 跨文件系统落盘用：os.Rename 失败（源与目标不在同一卷）时退回复制。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}

func (a *App) showStatus(status func(string) error, text string) {
	if err := status(text); err != nil {
		a.log.Errorln(err)
	}
}

// 错误详情可能含服务器 IP、下载直链等，只展示错误类型，详情由调用方记录日志
func (a *App) showDownloadFailed(status func(string) error, songInfo store.SongInfo, reason string) {
	a.showStatus(status, render.MusicStatus(songInfo.SongName, songInfo.SongAlbum, songInfo.FileExt, float64(songInfo.MusicSize)/1024/1024, reason+"\n"+render.RetryLater))
}

func (a *App) musicCaption(songInfo store.SongInfo) string {
	return render.MusicCaption(songInfo.SongName, songInfo.SongArtists, songInfo.SongAlbum, songInfo.FileExt,
		float64(songInfo.MusicSize+songInfo.EmbPicSize)/1024/1024, float64(songInfo.BitRate)/1000, songInfo.ProviderName, a.me.Username)
}

// cachedMusicResult 缓存歌曲用 CachedAudio 会被拒绝（AUDIO_CONTENT_TYPE_INVALID），须以文档形式发送
func (a *App) cachedMusicResult(id string, songInfo store.SongInfo, track *provider.Track) *models.InlineQueryResultCachedDocument {
	return &models.InlineQueryResultCachedDocument{
		ID:             id,
		DocumentFileID: songInfo.FileID,
		Title:          fmt.Sprintf("%s - %s", songInfo.SongArtists, songInfo.SongName),
		Description:    songInfo.SongAlbum,
		Caption:        a.musicCaption(songInfo),
		ParseMode:      models.ParseModeHTML,
		ReplyMarkup:    musicKeyboard(songInfo, track),
	}
}

func (a *App) sendMusic(ctx context.Context, songInfo store.SongInfo, track *provider.Track, musicPath, picPath string, chatID int64, reply *models.ReplyParameters) (*models.Message, error) {
	params := &tgb.SendAudioParams{
		ChatID:          chatID,
		Caption:         a.musicCaption(songInfo),
		ParseMode:       models.ParseModeHTML,
		Title:           songInfo.SongName,
		Performer:       songInfo.SongArtists,
		Duration:        songInfo.Duration,
		ReplyMarkup:     musicKeyboard(songInfo, track),
		ReplyParameters: reply,
	}
	if songInfo.FileID != "" {
		params.Audio = &models.InputFileString{Data: songInfo.FileID}
	} else {
		file, err := os.Open(musicPath)
		if err != nil {
			return nil, err
		}
		defer file.Close()
		params.Audio = &models.InputFileUpload{Filename: filepath.Base(musicPath), Data: file}
		_, _ = a.tg.SendChatAction(ctx, &tgb.SendChatActionParams{
			ChatID: chatID,
			Action: models.ChatActionUploadDocument,
		})
	}
	// Telegram 不接受以 file_id 复用缩略图，重发缓存歌曲时忽略 ThumbFileID
	if picPath != "" {
		picFile, err := os.Open(picPath)
		if err != nil {
			return nil, err
		}
		defer picFile.Close()
		params.Thumbnail = &models.InputFileUpload{Filename: filepath.Base(picPath), Data: picFile}
	}
	return a.tg.SendAudio(ctx, params)
}
