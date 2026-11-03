package main

import (
	"encoding/json"
	"fmt"
	"path"
	"strconv"
	"strings"

	marker "github.com/XiaoMengXinX/163KeyMarker"
	"github.com/XiaoMengXinX/Music163Api-Go/types"

	"github.com/XiaoMengXinX/TuneweaveBot/pluginsdk"
)

func songURL(id int) string     { return fmt.Sprintf("https://music.163.com/song?id=%d", id) }
func songPageURL(id int) string { return fmt.Sprintf("https://music.163.com/song/%d", id) }
func programURL(id int) string  { return fmt.Sprintf("https://music.163.com/program?id=%d", id) }

func match(text string) *Ref {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if id, err := strconv.Atoi(text); err == nil && id > 0 {
		return songRef(id)
	}
	if !containsMusicLink(text) {
		return nil
	}
	if containsShortLink(text) {
		text = resolveShortLink(client.http, text)
	}
	if !containsSongLink(text) {
		return nil
	}
	if id := parseMusicID(text); id != 0 {
		ref := songRef(id)
		ref.URL = songURL(id)
		return ref
	}
	if id := parseProgramID(text); id != 0 {
		return &Ref{Provider: providerName, ID: strconv.Itoa(id), Sub: "program", URL: programURL(id), PageURL: programURL(id)}
	}
	return nil
}

func songRef(id int) *Ref {
	return &Ref{
		Provider:  providerName,
		ID:        strconv.Itoa(id),
		PageURL:   songPageURL(id),
		ShareText: songURL(id),
	}
}

func canonicalize(ref Ref) (Ref, error) {
	if ref.Sub != "program" {
		ref.Provider = providerName
		if id, err := strconv.Atoi(ref.ID); err == nil && id > 0 {
			if ref.PageURL == "" {
				ref.PageURL = songPageURL(id)
			}
			if ref.ShareText == "" {
				ref.ShareText = songURL(id)
			}
		}
		return ref, nil
	}
	programID, err := strconv.Atoi(ref.ID)
	if err != nil || programID <= 0 {
		return Ref{}, pluginsdk.Fail("not_found", "invalid program id "+ref.ID)
	}
	prog, err := client.ProgramDetail(programID)
	if err != nil {
		return Ref{}, err
	}
	songID := prog.Program.MainSong.ID
	if songID == 0 {
		return Ref{}, pluginsdk.Fail("not_found", fmt.Sprintf("program %d has no main song", programID))
	}
	return Ref{
		Provider:  providerName,
		ID:        strconv.Itoa(songID),
		Sub:       strconv.Itoa(programID), // 记下来源节目，供 resolve 用节目详情补全歌曲元数据
		URL:       ref.URL,
		PageURL:   songPageURL(songID),
		ShareText: songURL(songID),
	}, nil
}

// resolveResult 把 Track 与 NoAudioError 一起返回，宿主据此保留元数据。
type resolveResult struct {
	Track   *Track   `json:"track"`
	NoAudio *NoAudio `json:"no_audio,omitempty"`
}

type NoAudio struct {
	Code   int    `json:"code"`
	Fee    int    `json:"fee"`
	Reason string `json:"reason,omitempty"`
}

// noAudioReason 把网易云 song/url 的 code 与 fee 转成给用户看的原因。
func noAudioReason(code, fee int) string {
	switch fee {
	case 1:
		return "需要网易云黑胶 VIP"
	case 4:
		return "需要单独购买专辑"
	}
	if code == 404 {
		return "无版权或已下架"
	}
	return fmt.Sprintf("网易云未提供下载地址（code %d）", code)
}

// coverSize 是封面请求的边长上限：网易云原图可达数千像素，CDN 按 param 缩小（不放大）。
const coverSize = "1000y1000"

func coverURL(pic string) string {
	if pic == "" || strings.Contains(pic, "param=") {
		return pic
	}
	sep := "?"
	if strings.Contains(pic, "?") {
		sep = "&"
	}
	return pic + sep + "param=" + coverSize
}

func resolve(ref Ref) (*resolveResult, error) {
	musicID, err := strconv.Atoi(ref.ID)
	if err != nil || musicID <= 0 {
		return nil, pluginsdk.Fail("not_found", "invalid song id "+ref.ID)
	}
	detail, urlData, err := client.SongDetailAndURL(musicID)
	if err != nil {
		return nil, err
	}
	// Sub 为来源节目 ID（canonicalize 写入）：节目主歌曲的 song/detail 常缺专辑/封面，
	// 用节目详情补全。
	if programID, perr := strconv.Atoi(ref.Sub); perr == nil && programID > 0 {
		prog, derr := client.ProgramDetail(programID)
		if derr != nil {
			return nil, derr
		}
		enrichFromProgram(&detail, &prog)
		track, noAudio := trackFromSong(Ref{ID: ref.ID, URL: ref.URL}, detail.Songs[0], urlData)
		track.PageURL = programURL(programID)
		track.ShareText = songURL(musicID)
		return &resolveResult{Track: track, NoAudio: noAudio}, nil
	}
	if len(detail.Songs) == 0 {
		return nil, pluginsdk.Fail("not_found", fmt.Sprintf("song %d", musicID))
	}
	track, noAudio := trackFromSong(ref, detail.Songs[0], urlData)
	return &resolveResult{Track: track, NoAudio: noAudio}, nil
}

func trackFromSong(ref Ref, song types.SongDetailData, urlData types.SongsURLData) (*Track, *NoAudio) {
	track := &Track{
		Ref:          Ref{Provider: providerName, ID: strconv.Itoa(song.Id), URL: ref.URL},
		Title:        song.Name,
		Artists:      artistNames(song),
		Album:        song.Al.Name,
		DurationMS:   song.Dt,
		CoverURL:     coverURL(song.Al.PicUrl),
		PageURL:      songPageURL(song.Id),
		ShareText:    songURL(song.Id),
		ProviderName: displayName,
		TagFormat:    "netease163key",
		Download: Policy{
			HostReplace: map[string]string{
				"m8.": "m7.", "m801.": "m701.", "m804.": "m701.", "m704.": "m701.",
			},
			ForceHTTPS:       true,
			ForceMultiThread: true,
		},
	}
	url := types.SongURLData{}
	if len(urlData.Data) != 0 {
		url = urlData.Data[0]
	}
	if payload, err := json.Marshal(marker.CreateMarker(song, url)); err == nil {
		track.TagPayload = payload
	}
	if len(urlData.Data) == 0 || urlData.Data[0].Url == "" {
		code, fee := urlData.Code, 0
		if len(urlData.Data) != 0 {
			code, fee = urlData.Data[0].Code, urlData.Data[0].Fee
		}
		return track, &NoAudio{Code: code, Fee: fee, Reason: noAudioReason(code, fee)}
	}
	track.AudioURL = url.Url
	track.Size = int64(url.Size)
	track.MD5 = url.Md5
	track.Ext = extFromURL(url.Url)
	if duration := song.Dt / 1000; duration > 0 {
		track.BitRate = int(8 * track.Size / int64(duration))
	}
	return track, nil
}

func search(keyword string, limit, offset int) ([]TrackSummary, int, error) {
	result, err := client.SearchSong(keyword, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	items := make([]TrackSummary, 0, len(result.Result.Songs))
	for _, song := range result.Result.Songs {
		names := make([]string, 0, len(song.Artists))
		for _, artist := range song.Artists {
			names = append(names, artist.Name)
		}
		items = append(items, TrackSummary{
			Ref:        *songRef(song.Id),
			Title:      song.Name,
			Artists:    names,
			Album:      song.Album.Name,
			DurationMS: song.Duration,
		})
	}
	return items, result.Result.SongCount, nil
}

func lyrics(ref Ref) (*Lyric, error) {
	if ref.Sub == "program" {
		return nil, pluginsdk.Fail("unsupported", "programs have no lyrics")
	}
	musicID, err := strconv.Atoi(ref.ID)
	if err != nil || musicID <= 0 {
		return nil, pluginsdk.Fail("not_found", "invalid song id "+ref.ID)
	}
	lyric, detail, err := client.LyricAndDetail(musicID)
	if err != nil {
		return nil, err
	}
	if lyric.Lrc.Lyric == "" || len(detail.Songs) == 0 {
		return nil, pluginsdk.Fail("not_found", fmt.Sprintf("lyrics for %d", musicID))
	}
	song := detail.Songs[0]
	return &Lyric{LRC: lyric.Lrc.Lyric, Title: song.Name, Artists: artistNames(song), Album: song.Al.Name}, nil
}

func artistNames(song types.SongDetailData) []string {
	names := make([]string, 0, len(song.Ar))
	for _, ar := range song.Ar {
		names = append(names, ar.Name)
	}
	return names
}

func extFromURL(rawURL string) string {
	if path.Ext(urlBase(rawURL)) == ".flac" {
		return "flac"
	}
	return "mp3"
}

func urlBase(rawURL string) string {
	if i := strings.IndexByte(rawURL, '?'); i >= 0 {
		rawURL = rawURL[:i]
	}
	if i := strings.IndexByte(rawURL, '#'); i >= 0 {
		rawURL = rawURL[:i]
	}
	return path.Base(rawURL)
}

// 以下别名必须与 types.SongDetailData 的字段类型一致
type songArtist = struct {
	Id    int           `json:"id"`
	Name  string        `json:"name"`
	Tns   []interface{} `json:"tns"`
	Alias []interface{} `json:"alias"`
}

type songAlbum = struct {
	Id     int           `json:"id"`
	Name   string        `json:"name"`
	PicUrl string        `json:"picUrl"`
	Tns    []interface{} `json:"tns"`
	PicStr string        `json:"pic_str"`
	Pic    int           `json:"pic"`
}

func enrichFromProgram(detail *types.SongsDetailData, prog *types.ProgramDetailData) {
	p := prog.Program
	if len(detail.Songs) == 0 {
		detail.Songs = []types.SongDetailData{{
			Id:   p.MainSong.ID,
			Name: p.MainSong.Name,
			Dt:   p.MainSong.Duration,
			Al:   songAlbum{Name: p.Name, PicUrl: p.CoverURL},
		}}
	}
	song := &detail.Songs[0]
	if song.Name == "" {
		song.Name = p.MainSong.Name
	}
	if song.Name == "" {
		song.Name = p.Name
	}
	if song.Dt == 0 {
		song.Dt = p.MainSong.Duration
	}
	if song.Al.Name == "" {
		song.Al.Name = p.Name
	}
	if song.Al.PicUrl == "" {
		song.Al.PicUrl = p.CoverURL
	}
	if !hasArtistNames(song.Ar) {
		song.Ar = programArtists(prog)
	}
}

func hasArtistNames(artists []songArtist) bool {
	for _, a := range artists {
		if a.Name != "" {
			return true
		}
	}
	return false
}

func programArtists(prog *types.ProgramDetailData) []songArtist {
	var artists []songArtist
	for _, a := range prog.Program.MainSong.Artists {
		artists = append(artists, songArtist{Id: a.ID, Name: a.Name})
	}
	if len(artists) == 0 && prog.Program.Dj.Nickname != "" {
		artists = append(artists, songArtist{Name: prog.Program.Dj.Nickname})
	}
	return artists
}
