package store

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// SongRequest 是一次点歌记录，SongID 指向 song_infos.id。CreatedAt 为 unix 秒。
type SongRequest struct {
	ID        uint
	CreatedAt int64
	UserID    int64
	ChatID    int64
	SongID    uint
	Source    string
}

func (SongRequest) TableName() string { return "song_requests" }

func (s *Store) RecordRequest(userID, chatID int64, songID uint, source string) error {
	return s.db.Create(&SongRequest{UserID: userID, ChatID: chatID, SongID: songID, Source: source}).Error
}

func (s *Store) GetByID(id uint) (SongInfo, bool) {
	var songInfo SongInfo
	if err := s.db.First(&songInfo, id).Error; err != nil {
		return songInfo, false
	}
	return songInfo, true
}

// CacheTotals 返回缓存歌曲的数量与文件总大小（字节）。
func (s *Store) CacheTotals() (songs, size int64, err error) {
	var out struct{ Songs, Size int64 }
	err = s.db.Raw(`SELECT COUNT(*) AS songs, COALESCE(SUM(` + songSizeExpr + `), 0) AS size
		FROM song_infos WHERE deleted_at IS NULL`).Scan(&out).Error
	return out.Songs, out.Size, err
}

// songSizeExpr 是一首歌发送到 Telegram 的文件大小：音频加嵌入的封面。
// 导入的旧数据里这两列可能为 NULL。
const songSizeExpr = `COALESCE(music_size, 0) + COALESCE(emb_pic_size, 0)`

// UserSummary 是某个用户点过的歌曲（去重）的汇总。
type UserSummary struct {
	Songs    int64 `json:"songs"`
	Duration int64 `json:"duration"` // 秒
	Size     int64 `json:"size"`     // 字节
}

func (s *Store) UserSummary(userID int64) (UserSummary, error) {
	var out UserSummary
	err := s.db.Raw(`SELECT COUNT(*) AS songs, COALESCE(SUM(duration), 0) AS duration,
		COALESCE(SUM(`+songSizeExpr+`), 0) AS size
		FROM song_infos WHERE deleted_at IS NULL
		AND id IN (SELECT DISTINCT song_id FROM song_requests WHERE user_id = ?)`, userID).Scan(&out).Error
	return out, err
}

// UserSong 是用户点过的一首歌及其点歌情况。
type UserSong struct {
	SongInfo
	LastAt int64 // 最近一次点歌的 unix 秒
	Times  int64
}

// SongOrder 是用户歌曲列表的排序方式。
type SongOrder string

const (
	OrderRecent    SongOrder = "recent" // 最近点的在前
	OrderOldest    SongOrder = "oldest" // 最早点的在前
	OrderTitleAsc  SongOrder = "az"     // 按歌名 A-Z
	OrderTitleDesc SongOrder = "za"     // 按歌名 Z-A
)

// songOrderSQL 是各排序方式的 ORDER BY 子句。歌名按 Unicode 码位比较，英文不区分大小写；
// 中文按码位而不是拼音排序，排在英文之后。歌名相同时最近点的在前。
var songOrderSQL = map[SongOrder]string{
	OrderRecent:    "r.last_at DESC, s.id DESC",
	OrderOldest:    "r.last_at ASC, s.id ASC",
	OrderTitleAsc:  "s.song_name COLLATE NOCASE ASC, r.last_at DESC, s.id DESC",
	OrderTitleDesc: "s.song_name COLLATE NOCASE DESC, r.last_at DESC, s.id DESC",
}

// UserSongs 列出用户点过的歌曲，未知的 order 按 OrderRecent 处理；query 非空时匹配歌名、歌手与专辑。
func (s *Store) UserSongs(userID int64, query string, order SongOrder, limit, offset int) (songs []UserSong, total int64, err error) {
	base := func() *gorm.DB {
		tx := s.db.Table("song_infos AS s").
			Joins(`JOIN (SELECT song_id, MAX(created_at) AS last_at, COUNT(*) AS times
				FROM song_requests WHERE user_id = ? GROUP BY song_id) AS r ON r.song_id = s.id`, userID).
			Where("s.deleted_at IS NULL")
		return matchSong(tx, "s.", query)
	}
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	orderBy, ok := songOrderSQL[order]
	if !ok {
		orderBy = songOrderSQL[OrderRecent]
	}
	err = base().Select("s.*, r.last_at, r.times").Order(orderBy).Limit(limit).Offset(offset).Scan(&songs).Error
	return songs, total, err
}

// ListSongs 按缓存时间倒序列出缓存，可按关键词与 provider 过滤。
func (s *Store) ListSongs(query, provider string, limit, offset int) (songs []SongInfo, total int64, err error) {
	base := func() *gorm.DB {
		tx := s.db.Model(&SongInfo{})
		if provider != "" {
			tx = tx.Where("provider = ?", provider)
		}
		return matchSong(tx, "", query)
	}
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err = base().Order("id DESC").Limit(limit).Offset(offset).Find(&songs).Error
	return songs, total, err
}

// RequestCount 返回一首歌被点的次数。
func (s *Store) RequestCount(songID uint) (n int64, err error) {
	err = s.db.Model(&SongRequest{}).Where("song_id = ?", songID).Count(&n).Error
	return n, err
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func matchSong(tx *gorm.DB, prefix, query string) *gorm.DB {
	query = strings.TrimSpace(query)
	if query == "" {
		return tx
	}
	pattern := "%" + likeEscaper.Replace(query) + "%"
	cond := prefix + `song_name LIKE ? ESCAPE '\' OR ` + prefix + `song_artists LIKE ? ESCAPE '\' OR ` +
		prefix + `song_album LIKE ? ESCAPE '\' OR ` + prefix + `track_id = ?`
	return tx.Where(cond, pattern, pattern, pattern, query)
}

// DayCount 是某一天（本地日期 YYYY-MM-DD）的计数。
type DayCount struct {
	Day   string `json:"day"`
	Count int64  `json:"count"`
}

// NamedCount 是按某个维度分组的计数。
type NamedCount struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// IDCount 是按用户或聊天 ID 分组的计数。
type IDCount struct {
	ID    int64 `json:"id"`
	Count int64 `json:"count"`
}

// SongCount 是一首歌在统计区间内被点的次数。
type SongCount struct {
	SongInfo
	Count int64
}

// Stats 是统计区间内的全站聚合；Provider 非空时只统计该 provider 的歌曲。
type Stats struct {
	Cached             []DayCount // 每日新增缓存
	Requests           []DayCount // 每日点歌量
	TopSongs           []SongCount
	TopUsers           []IDCount
	TopChats           []IDCount
	CacheByProvider    []NamedCount // 全部缓存按 provider 分布，不受区间与 Provider 过滤影响
	RequestsByProvider []NamedCount
	RequestsBySource   []NamedCount
}

const statsTopN = 10

// Stats 聚合最近 days 天（含今天，按 now 所在时区划分自然日）的数据，缺数据的日期补零。
func (s *Store) Stats(now time.Time, days int, provider string) (Stats, error) {
	var out Stats
	_, offset := now.Zone()
	y, m, d := now.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1))
	startDay := start.Format(time.DateOnly)
	// 时区偏移取当前值；区间跨越夏令时切换时，切换前的日期边界会偏一小时

	providerCond, providerArgs := "", []any{}
	if provider != "" {
		providerCond, providerArgs = " AND s.provider = ?", []any{provider}
	}
	args := func(first ...any) []any { return append(first, providerArgs...) }

	// created_at 是带时区偏移的文本（导入的旧数据为 +08:00，新数据为写入时的时区），不能直接按
	// 字符串比较时间。先用字符串粗过滤让查询走 idx_song_infos_live_created：文本开头的日期与换算到
	// 当前时区后的日期最多相差两天，所以提前两天的下界不会漏掉行；再用 date() 精确判断。
	roughStart := start.AddDate(0, 0, -2).Format(time.DateOnly)
	var cached []DayCount
	if err := s.db.Raw(`SELECT date(s.created_at, ?) AS day, COUNT(*) AS count FROM song_infos AS s
		WHERE s.deleted_at IS NULL AND s.created_at >= ? AND date(s.created_at, ?) >= ?`+providerCond+` GROUP BY day`,
		args(secondsModifier(offset), roughStart, secondsModifier(offset), startDay)...).Scan(&cached).Error; err != nil {
		return out, err
	}
	out.Cached = fillDays(cached, start, days)

	// 以下查询都限定在区间内的点歌记录，并关联未删除的歌曲
	const from = ` FROM song_requests AS r JOIN song_infos AS s ON s.id = r.song_id
		WHERE r.created_at >= ? AND s.deleted_at IS NULL`
	since := start.Unix()

	var requests []DayCount
	if err := s.db.Raw(`SELECT date(r.created_at, 'unixepoch', ?) AS day, COUNT(*) AS count`+from+providerCond+` GROUP BY day`,
		append([]any{secondsModifier(offset)}, args(since)...)...).Scan(&requests).Error; err != nil {
		return out, err
	}
	out.Requests = fillDays(requests, start, days)

	if err := s.db.Raw(`SELECT s.*, COUNT(*) AS count`+from+providerCond+
		` GROUP BY s.id ORDER BY count DESC, s.id DESC LIMIT ?`, append(args(since), statsTopN)...).Scan(&out.TopSongs).Error; err != nil {
		return out, err
	}
	for _, group := range []struct {
		column string
		dest   *[]IDCount
	}{{"r.user_id", &out.TopUsers}, {"r.chat_id", &out.TopChats}} {
		if err := s.db.Raw(`SELECT `+group.column+` AS id, COUNT(*) AS count`+from+providerCond+
			` GROUP BY `+group.column+` ORDER BY count DESC, `+group.column+` LIMIT ?`, append(args(since), statsTopN)...).Scan(group.dest).Error; err != nil {
			return out, err
		}
	}
	if err := s.db.Raw(`SELECT r.source AS key, COUNT(*) AS count`+from+providerCond+
		` GROUP BY r.source ORDER BY count DESC`, args(since)...).Scan(&out.RequestsBySource).Error; err != nil {
		return out, err
	}
	if err := s.db.Raw(`SELECT s.provider AS key, COUNT(*) AS count`+from+
		` GROUP BY s.provider ORDER BY count DESC`, since).Scan(&out.RequestsByProvider).Error; err != nil {
		return out, err
	}
	err := s.db.Raw(`SELECT provider AS key, COUNT(*) AS count FROM song_infos
		WHERE deleted_at IS NULL GROUP BY provider ORDER BY count DESC`).Scan(&out.CacheByProvider).Error
	return out, err
}

// secondsModifier 是 SQLite 日期函数的时区偏移修饰符，如 "+28800 seconds"。
func secondsModifier(offset int) string {
	return fmt.Sprintf("%+d seconds", offset)
}

// fillDays 把查询结果铺到从 start 起连续 days 天上，缺失的日期计 0。
func fillDays(rows []DayCount, start time.Time, days int) []DayCount {
	counts := make(map[string]int64, len(rows))
	for _, r := range rows {
		counts[r.Day] = r.Count
	}
	out := make([]DayCount, days)
	for i := range out {
		day := start.AddDate(0, 0, i).Format(time.DateOnly)
		out[i] = DayCount{Day: day, Count: counts[day]}
	}
	return out
}
