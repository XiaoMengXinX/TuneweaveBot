package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/XiaoMengXinX/Music163Api-Go/api"
	"github.com/XiaoMengXinX/Music163Api-Go/types"
	"github.com/XiaoMengXinX/Music163Api-Go/utils"

	"github.com/XiaoMengXinX/TuneweaveBot/pluginsdk"
)

// searchCooldown 是某个搜索接口触发风控后被跳过的时长。网易云按接口路径分别计数，
// 实测被限的接口静置十几分钟后恢复。
const searchCooldown = 15 * time.Minute

// apiClient 是 Music163Api-Go 的薄封装，HTTP 请求经宿主代理。
type apiClient struct {
	mu     sync.RWMutex
	musicU string
	level  string
	http   *http.Client
	now    func() time.Time
	// limitedUntil[i] 是 searchEndpoints[i] 冷却结束的时间，由 mu 保护
	limitedUntil [len(searchEndpoints)]time.Time
}

func newAPIClient(client *http.Client, level string) *apiClient {
	return &apiClient{level: level, http: client, now: time.Now}
}

func (c *apiClient) SetMUSICU(musicU string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.musicU = musicU
}

func (c *apiClient) hasMUSICU() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.musicU != ""
}

func (c *apiClient) requestData() utils.RequestData {
	c.mu.RLock()
	defer c.mu.RUnlock()
	data := utils.RequestData{HTTPClient: c.http}
	if c.musicU != "" {
		data.Cookies = []*http.Cookie{{Name: "MUSIC_U", Value: c.musicU}}
	}
	return data
}

func (c *apiClient) SongDetailAndURL(musicID int) (types.SongsDetailData, types.SongsURLData, error) {
	var detail types.SongsDetailData
	var urlData types.SongsURLData
	b := api.NewBatch(
		api.BatchAPI{
			Key:  api.SongDetailAPI,
			Json: api.CreateSongDetailReqJson([]int{musicID}),
		},
		api.BatchAPI{
			Key:  api.SongUrlAPI,
			Json: api.CreateSongURLJson(api.SongURLConfig{Ids: []int{musicID}, Level: c.level}),
		},
	)
	if err := b.Do(c.requestData()).Error; err != nil {
		return detail, urlData, err
	}
	_, result := b.Parse()
	if err := json.Unmarshal([]byte(result[api.SongDetailAPI]), &detail); err != nil {
		return detail, urlData, err
	}
	if err := json.Unmarshal([]byte(result[api.SongUrlAPI]), &urlData); err != nil {
		return detail, urlData, err
	}
	return detail, urlData, nil
}

func (c *apiClient) LyricAndDetail(musicID int) (types.SongLyricData, types.SongsDetailData, error) {
	var lyric types.SongLyricData
	var detail types.SongsDetailData
	b := api.NewBatch(
		api.BatchAPI{
			Key:  api.SongLyricAPI,
			Json: api.CreateSongLyricReqJson(musicID),
		},
		api.BatchAPI{
			Key:  api.SongDetailAPI,
			Json: api.CreateSongDetailReqJson([]int{musicID}),
		},
	)
	if err := b.Do(c.requestData()).Error; err != nil {
		return lyric, detail, err
	}
	_, result := b.Parse()
	if err := json.Unmarshal([]byte(result[api.SongLyricAPI]), &lyric); err != nil {
		return lyric, detail, err
	}
	if err := json.Unmarshal([]byte(result[api.SongDetailAPI]), &detail); err != nil {
		return lyric, detail, err
	}
	return lyric, detail, nil
}

type searchEndpoint func(utils.RequestData, api.SearchSongConfig) (items []TrackSummary, total, code int, err error)

// searchEndpoints 按优先级排列。实测三个接口对同一关键词返回的歌曲与顺序一致，
// 翻页途中换接口不会导致重复或遗漏。
var searchEndpoints = [...]searchEndpoint{
	func(data utils.RequestData, cfg api.SearchSongConfig) ([]TrackSummary, int, int, error) {
		res, err := api.SearchSong(data, cfg)
		return searchSongDataToSummaries(res), res.Result.SongCount, res.Code, err
	},
	func(data utils.RequestData, cfg api.SearchSongConfig) ([]TrackSummary, int, int, error) {
		res, err := api.SearchSongGet(data, cfg)
		return searchSongDataToSummaries(res), res.Result.SongCount, res.Code, err
	},
	func(data utils.RequestData, cfg api.SearchSongConfig) ([]TrackSummary, int, int, error) {
		res, err := api.CloudSearchSong(data, cfg)
		return cloudSearchDataToSummaries(res), res.Result.SongCount, res.Code, err
	},
}

// isRateLimitCode 判断搜索接口是否返回了风控（操作频繁）。
func isRateLimitCode(code int) bool {
	return code == 405 || code == 406
}

func (c *apiClient) searchAvailable(i int, now time.Time) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return now.After(c.limitedUntil[i])
}

func (c *apiClient) coolDown(i int, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limitedUntil[i] = now.Add(searchCooldown)
}

// SearchSongs 依次尝试各搜索接口，只有遇到风控才换下一个，并让被限的接口冷却一段时间；
// 网络错误与其他错误码直接返回。全部接口都在冷却时仍尝试最后一个接口。
func (c *apiClient) SearchSongs(keyword string, limit, offset int) ([]TrackSummary, int, error) {
	cfg := api.SearchSongConfig{Keyword: keyword, Limit: limit, Offset: offset}
	data := c.requestData()
	now := c.now()
	var lastErr error
	tried := false
	for i, endpoint := range searchEndpoints {
		last := i == len(searchEndpoints)-1
		if !c.searchAvailable(i, now) && (tried || !last) {
			continue
		}
		tried = true
		items, total, code, err := endpoint(data, cfg)
		if err != nil {
			return nil, 0, err
		}
		if code == 200 {
			return items, total, nil
		}
		lastErr = fmt.Errorf("netease search error (code %d)", code)
		if !isRateLimitCode(code) {
			return nil, 0, lastErr
		}
		c.coolDown(i, now)
	}
	return nil, 0, pluginsdk.Fail("rate_limited", lastErr.Error())
}

func (c *apiClient) ProgramDetail(programID int) (types.ProgramDetailData, error) {
	return api.GetProgramDetail(c.requestData(), programID)
}

// 扫码登录

const (
	qrExpired = 800
	qrWaiting = 801
	qrScanned = 802
	qrSuccess = 803
)

func (c *apiClient) CreateQRLoginKey() (string, error) {
	result, err := api.GetQrUnikey(utils.RequestData{HTTPClient: c.http})
	if err != nil {
		return "", err
	}
	if result.Unikey == "" {
		return "", fmt.Errorf("获取扫码登录 key 失败: %s", result.RawJson)
	}
	return result.Unikey, nil
}

func qrLoginContent(unikey string) string {
	return "https://music.163.com/login?codekey=" + unikey
}

// CheckQRLogin 匿名查询扫码状态，成功时返回新的 MUSIC_U。
func (c *apiClient) CheckQRLogin(key string) (status int, nickname, musicU string, err error) {
	result, header, err := api.CheckQrLogin(utils.RequestData{HTTPClient: c.http}, key)
	if err != nil {
		return 0, "", "", err
	}
	switch result.Code {
	case qrExpired:
		return qrExpired, "", "", nil
	case qrWaiting:
		return qrWaiting, "", "", nil
	case qrScanned:
		return qrScanned, result.Nickname, "", nil
	case qrSuccess:
		musicU := extractMUSICU(header)
		if musicU == "" {
			return 0, "", "", fmt.Errorf("登录成功但未从响应中解析到 MUSIC_U")
		}
		return qrSuccess, result.Nickname, musicU, nil
	}
	return 0, "", "", fmt.Errorf("扫码登录返回未知状态 %d: %s", result.Code, result.Message)
}

func extractMUSICU(header http.Header) string {
	for _, cookie := range header.Values("Set-Cookie") {
		first := strings.SplitN(cookie, ";", 2)[0]
		if name, value, ok := strings.Cut(strings.TrimSpace(first), "="); ok && name == "MUSIC_U" {
			return value
		}
	}
	return ""
}

func (c *apiClient) LoginStatusValid() (bool, error) {
	result, err := api.GetLoginStatus(c.requestData())
	if err != nil {
		return false, err
	}
	return result.Code == 200 && result.Profile.UserId != 0, nil
}
