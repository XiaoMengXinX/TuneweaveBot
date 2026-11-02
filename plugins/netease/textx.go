package main

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	reg1   = regexp.MustCompile(`(.*)song\?id=`)
	reg2   = regexp.MustCompile(`(.*)song/`)
	regP1  = regexp.MustCompile(`(.*)program\?id=`)
	regP2  = regexp.MustCompile(`(.*)program/`)
	regP3  = regexp.MustCompile(`(.*)dj\?id=`)
	regP4  = regexp.MustCompile(`(.*)dj/`)
	reg3   = regexp.MustCompile(`\?(.*)`)
	reg4   = regexp.MustCompile("&(.*)")
	reg5   = regexp.MustCompile("/(.*)")
	regInt = regexp.MustCompile(`\d+`)
	regURL = regexp.MustCompile(`(http|https)://[\w\-]+(\.[\w\-]+)+([\w\-.,@?^=%&:/~+#]*[\w\-@?^=%&/~+#])?`)
)

func containsMusicLink(text string) bool {
	return strings.Contains(text, "music.163.com") ||
		strings.Contains(text, "163cn.tv") ||
		strings.Contains(text, "163cn.link")
}

func containsSongLink(text string) bool {
	return strings.Contains(text, "song") ||
		strings.Contains(text, "program") ||
		strings.Contains(text, "dj")
}

func containsShortLink(text string) bool {
	return strings.Contains(text, "163cn.tv") || strings.Contains(text, "163cn.link")
}

// resolveShortLink 只取第一次跳转的 Location，失败时原样返回。
func resolveShortLink(client *http.Client, text string) string {
	messageText := strings.NewReplacer("\n", "", " ", "").Replace(text)
	musicURL := regURL.FindStringSubmatch(messageText)
	if len(musicURL) == 0 {
		return text
	}
	u, err := url.Parse(musicURL[0])
	if err != nil || (u.Host != "163cn.tv" && u.Host != "163cn.link") {
		return text
	}
	c := *client
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := c.Get(musicURL[0])
	if err != nil {
		return text
	}
	defer resp.Body.Close()
	return resp.Header.Get("Location")
}

func parseMusicID(text string) int {
	messageText := strings.NewReplacer("\n", "", " ", "").Replace(text)
	if musicURL := regURL.FindStringSubmatch(messageText); len(musicURL) != 0 {
		if strings.Contains(musicURL[0], "song") {
			ur, _ := url.Parse(musicURL[0])
			if musicID, _ := strconv.Atoi(ur.Query().Get("id")); musicID != 0 {
				return musicID
			}
		}
	}
	musicID, _ := strconv.Atoi(linkTestMusic(messageText))
	return musicID
}

func parseProgramID(text string) int {
	messageText := strings.NewReplacer("\n", "", " ", "").Replace(text)
	if programURL := regURL.FindStringSubmatch(messageText); len(programURL) != 0 {
		if strings.Contains(programURL[0], "program") {
			ur, _ := url.Parse(programURL[0])
			if id, _ := strconv.Atoi(ur.Query().Get("id")); id != 0 {
				return id
			}
		}
	}
	programID, _ := strconv.Atoi(linkTestProgram(messageText))
	return programID
}

func extractInt(text string) string {
	matchArr := regInt.FindStringSubmatch(text)
	if len(matchArr) == 0 {
		return ""
	}
	return matchArr[0]
}

func linkTestMusic(text string) string {
	return extractInt(reg5.ReplaceAllString(reg4.ReplaceAllString(reg3.ReplaceAllString(reg2.ReplaceAllString(reg1.ReplaceAllString(text, ""), ""), ""), ""), ""))
}

func linkTestProgram(text string) string {
	return extractInt(reg5.ReplaceAllString(reg4.ReplaceAllString(reg3.ReplaceAllString(regP4.ReplaceAllString(regP3.ReplaceAllString(regP2.ReplaceAllString(regP1.ReplaceAllString(text, ""), ""), ""), ""), ""), ""), ""))
}
