package webapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func sign(token, dataCheck string) string {
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(dataCheck))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestValidateInitData(t *testing.T) {
	const token = "1:secret"
	now := time.Unix(1_800_000_000, 0)
	user := `{"id":42,"first_name":"A","username":"a"}`
	build := func(authDate, hashToken string) string {
		check := "auth_date=" + authDate + "\nquery_id=q\nuser=" + user
		return url.Values{
			"auth_date": {authDate}, "query_id": {"q"}, "user": {user}, "hash": {sign(hashToken, check)},
		}.Encode()
	}

	got, err := ValidateInitData(build("1799999000", token), token, now)
	if err != nil || got.ID != 42 || got.Username != "a" {
		t.Fatalf("合法 initData 校验失败: %+v, %v", got, err)
	}
	for name, data := range map[string]string{
		"其他 token 签名": build("1799999000", "2:other"),
		"已过期":         build("1790000000", token),
		"缺少 hash":     "auth_date=1799999000&user=" + url.QueryEscape(user),
		"空":           "",
	} {
		if _, err := ValidateInitData(data, token, now); err == nil {
			t.Errorf("%s: 应校验失败", name)
		}
	}
}

func TestStatic(t *testing.T) {
	if !Built() {
		t.Skip("未构建前端")
	}
	srv := httptest.NewServer(Static())
	defer srv.Close()
	for path, wantType := range map[string]string{
		"/":             "text/html",
		"/unknown/path": "text/html", // 未知路径回落到 index.html
	} {
		res, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if got := res.Header.Get("Content-Type"); res.StatusCode != http.StatusOK || !strings.HasPrefix(got, wantType) {
			t.Errorf("%s: status %d, Content-Type %q", path, res.StatusCode, got)
		}
	}
	res, err := http.Get(srv.URL + "/assets/missing.js")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	// 404 不能带长期缓存头，否则边缘会把它当作资源文件缓存下来
	if cc := res.Header.Get("Cache-Control"); res.StatusCode != http.StatusNotFound || strings.Contains(cc, "immutable") {
		t.Errorf("缺失的资源文件应返回不可长期缓存的 404, got %d, Cache-Control %q", res.StatusCode, cc)
	}
}
