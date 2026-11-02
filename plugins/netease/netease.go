package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/XiaoMengXinX/Music163Api-Go/api"
	"github.com/XiaoMengXinX/Music163Api-Go/types"
	"github.com/XiaoMengXinX/Music163Api-Go/utils"
)

// apiClient 是 Music163Api-Go 的薄封装，HTTP 请求经宿主代理。
type apiClient struct {
	mu     sync.RWMutex
	musicU string
	level  string
	http   *http.Client
}

func newAPIClient(client *http.Client, level string) *apiClient {
	return &apiClient{level: level, http: client}
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

func (c *apiClient) SearchSong(keyword string, limit, offset int) (types.SearchSongData, error) {
	return api.SearchSong(c.requestData(), api.SearchSongConfig{Keyword: keyword, Limit: limit, Offset: offset})
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
