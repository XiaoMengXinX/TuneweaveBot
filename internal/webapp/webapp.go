// Package webapp 提供 Mini App 看板的前端资源与 initData 校验。
package webapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// dist 是 web/ 的构建产物；未构建前端时只有占位文件。
//
//go:embed all:dist
var dist embed.FS

// Built 报告二进制是否带有前端构建产物。
func Built() bool {
	_, err := fs.Stat(dist, "dist/index.html")
	return err == nil
}

// Static 提供前端文件，未知路径回落到 index.html。
func Static() http.Handler {
	root, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServerFS(root)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if strings.HasPrefix(name, "assets/") {
			// 文件名带内容哈希
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			files.ServeHTTP(w, r)
			return
		}
		if name == "" || !fileExists(root, name) {
			name = "index.html"
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFileFS(w, r, root, name)
	})
}

func fileExists(root fs.FS, name string) bool {
	info, err := fs.Stat(root, name)
	return err == nil && !info.IsDir()
}

// User 是 initData 中的 Telegram 用户。
type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
}

// initDataMaxAge 是 initData 的有效期。initData 在 Mini App 打开时签发且之后不变，
// 限制有效期可以避免泄露的 initData 被长期重放。
const initDataMaxAge = 24 * time.Hour

var ErrInvalidInitData = errors.New("invalid init data")

// ValidateInitData 按 Telegram 文档校验 Mini App 的 initData 并返回其中的用户：
// 以 "WebAppData" 为 key 对 bot token 做 HMAC-SHA256 得到密钥，再对排序后的字段签名。
func ValidateInitData(initData, token string, now time.Time) (User, error) {
	values, err := url.ParseQuery(initData)
	if err != nil {
		return User{}, ErrInvalidInitData
	}
	hash, err := hex.DecodeString(values.Get("hash"))
	if err != nil || len(hash) == 0 {
		return User{}, ErrInvalidInitData
	}
	pairs := make([]string, 0, len(values))
	for k, v := range values {
		if k != "hash" {
			pairs = append(pairs, k+"="+v[0])
		}
	}
	sort.Strings(pairs)

	secret := hmac.New(sha256.New, []byte("WebAppData"))
	secret.Write([]byte(token))
	mac := hmac.New(sha256.New, secret.Sum(nil))
	mac.Write([]byte(strings.Join(pairs, "\n")))
	if !hmac.Equal(mac.Sum(nil), hash) {
		return User{}, ErrInvalidInitData
	}

	authDate, err := strconv.ParseInt(values.Get("auth_date"), 10, 64)
	if err != nil || now.Sub(time.Unix(authDate, 0)) > initDataMaxAge {
		return User{}, ErrInvalidInitData
	}
	var user User
	if err := json.Unmarshal([]byte(values.Get("user")), &user); err != nil || user.ID == 0 {
		return User{}, ErrInvalidInitData
	}
	return user, nil
}
