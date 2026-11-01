package main

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/XiaoMengXinX/TuneweaveBot/pluginsdk"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func setTestClient(t *testing.T, handler roundTripFunc) {
	t.Helper()
	client = newAPIClient(&http.Client{Transport: handler}, "hires")
	t.Cleanup(func() { client = nil })
}

func jsonResponse(body string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
}

func TestMatch(t *testing.T) {
	setTestClient(t, func(*http.Request) (*http.Response, error) {
		t.Error("Match 不应发起网络请求")
		return nil, http.ErrNotSupported
	})
	for _, tt := range []struct {
		text string
		want *Ref
	}{
		{text: "123", want: &Ref{Provider: providerName, ID: "123", PageURL: songPageURL(123), ShareText: songURL(123)}},
		{text: "https://music.163.com/song?id=456&userid=1", want: &Ref{Provider: providerName, ID: "456", URL: songURL(456), PageURL: songPageURL(456), ShareText: songURL(456)}},
		{text: "https://music.163.com/#/song?id=789", want: &Ref{Provider: providerName, ID: "789", URL: songURL(789), PageURL: songPageURL(789), ShareText: songURL(789)}},
		{text: "https://music.163.com/program?id=2061", want: &Ref{Provider: providerName, ID: "2061", Sub: "program", URL: programURL(2061), PageURL: programURL(2061)}},
		{text: "https://music.163.com/dj?id=2061", want: &Ref{Provider: providerName, ID: "2061", Sub: "program", URL: programURL(2061), PageURL: programURL(2061)}},
		{text: "雨き声残響"},
		{text: "0"},
		{text: ""},
	} {
		ref := match(tt.text)
		if tt.want == nil {
			if ref != nil {
				t.Errorf("match(%q) = %+v, want nil", tt.text, ref)
			}
			continue
		}
		if ref == nil || *ref != *tt.want {
			t.Errorf("match(%q) = %+v, want %+v", tt.text, ref, tt.want)
		}
	}
}

func TestCanonicalize(t *testing.T) {
	setTestClient(t, func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/eapi/dj/program/detail" {
			t.Errorf("unexpected request %s", r.URL)
		}
		return jsonResponse(`{"code":200,"program":{"id":2061,"mainSong":{"id":123}}}`), nil
	})
	ref, err := canonicalize(Ref{Provider: providerName, ID: "2061", Sub: "program"})
	if err != nil || ref.ID != "123" || ref.Sub != "" || ref.PageURL != songPageURL(123) || ref.ShareText != songURL(123) {
		t.Fatalf("canonicalize = %+v, %v", ref, err)
	}
	if ref, err := canonicalize(Ref{Provider: providerName, ID: "1"}); err != nil || ref.PageURL != songPageURL(1) {
		t.Errorf("song ref = %+v, %v", ref, err)
	}
	if _, err := canonicalize(Ref{ID: "bad", Sub: "program"}); err == nil {
		t.Error("invalid program id should fail")
	}
}

func TestResolve(t *testing.T) {
	const detail = `{"songs":[{"id":123,"name":"Song","dt":2000,"ar":[{"name":"A"}],"al":{"name":"Album","picUrl":"https://p1.music.126.net/a.jpg"}}]}`
	setTestClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(`{"/api/v3/song/detail":` + detail + `,"/api/song/enhance/player/url/v1":{"code":200,"data":[{"url":"https://m8.music.126.net/1.mp3","size":32000,"md5":"abc"}]}}`), nil
	})
	result, err := resolve(Ref{ID: "123"})
	if err != nil {
		t.Fatal(err)
	}
	if result.NoAudio != nil {
		t.Fatalf("unexpected no_audio: %+v", result.NoAudio)
	}
	track := result.Track
	if track.AudioURL != "https://m8.music.126.net/1.mp3" || track.Ext != "mp3" || track.Size != 32000 || track.MD5 != "abc" {
		t.Errorf("track = %+v", track)
	}
	if track.BitRate != 128000 || track.ProviderName != displayName || track.TagFormat != "netease163key" {
		t.Errorf("track metadata = %+v", track)
	}
	if track.PageURL != songPageURL(123) || track.ShareText != songURL(123) {
		t.Errorf("track links = %q / %q", track.PageURL, track.ShareText)
	}
	if track.Download.HostReplace["m8."] != "m7." || !track.Download.ForceHTTPS {
		t.Errorf("download policy = %+v", track.Download)
	}
	if len(track.TagPayload) == 0 {
		t.Error("missing tag payload")
	}
}

func TestResolveNoAudio(t *testing.T) {
	const detail = `{"songs":[{"id":123,"name":"Song","dt":1000,"ar":[{"name":"A"}],"al":{"name":"Album"}}]}`
	setTestClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(`{"/api/v3/song/detail":` + detail + `,"/api/song/enhance/player/url/v1":{"code":200,"data":[{"url":"","code":404,"fee":1}]}}`), nil
	})
	result, err := resolve(Ref{ID: "123"})
	if err != nil {
		t.Fatal(err)
	}
	if result.NoAudio == nil || result.NoAudio.Code != 404 || result.NoAudio.Fee != 1 {
		t.Fatalf("no_audio = %+v", result.NoAudio)
	}
	if result.Track == nil || result.Track.Title != "Song" || result.Track.AudioURL != "" {
		t.Errorf("track = %+v", result.Track)
	}
}

func TestResolveNotFound(t *testing.T) {
	setTestClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(`{"/api/v3/song/detail":{"songs":[]},"/api/song/enhance/player/url/v1":{"data":[]}}`), nil
	})
	_, err := resolve(Ref{ID: "123"})
	var pe *pluginsdk.Error
	if !errors.As(err, &pe) || pe.Code != "not_found" {
		t.Errorf("err = %v, want not_found", err)
	}
}

func TestSearch(t *testing.T) {
	setTestClient(t, func(r *http.Request) (*http.Response, error) {
		return jsonResponse(`{"code":200,"result":{"songCount":1,"songs":[{"id":123,"name":"Song","duration":1000,"artists":[{"name":"A"},{"name":"B"}],"album":{"name":"Album"}}]}}`), nil
	})
	items, total, err := search("keyword", 10, 0)
	if err != nil || total != 1 || len(items) != 1 {
		t.Fatalf("search = %+v, %d, %v", items, total, err)
	}
	item := items[0]
	if item.ID != "123" || item.Title != "Song" || item.Album != "Album" || strings.Join(item.Artists, "/") != "A/B" {
		t.Errorf("item = %+v", item)
	}
	if item.PageURL != songPageURL(123) || item.ShareText != songURL(123) {
		t.Errorf("item links = %q / %q", item.PageURL, item.ShareText)
	}
}

func TestLyrics(t *testing.T) {
	setTestClient(t, func(*http.Request) (*http.Response, error) {
		return jsonResponse(`{"/api/song/lyric":{"lrc":{"lyric":"[00:00.00]lyric"}},"/api/v3/song/detail":{"songs":[{"id":123,"name":"Song","ar":[{"name":"A"}],"al":{"name":"Album"}}]}}`), nil
	})
	lyric, err := lyrics(Ref{ID: "123"})
	if err != nil || lyric.LRC != "[00:00.00]lyric" || lyric.Title != "Song" || lyric.Album != "Album" {
		t.Fatalf("lyrics = %+v, %v", lyric, err)
	}
	if _, err := lyrics(Ref{ID: "1", Sub: "program"}); err == nil {
		t.Error("program lyrics should fail")
	}
}
