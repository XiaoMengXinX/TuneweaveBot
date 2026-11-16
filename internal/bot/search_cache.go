package bot

import (
	"context"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/XiaoMengXinX/TuneweaveBot/internal/provider"
)

const (
	// searchFetchLimit 是一次搜索取回的条数，等于 bot 最多会展示的结果数（/search 的全部页）。
	searchFetchLimit = searchMaxPage * searchPageSize
	// searchCacheTTL 与搜索结果按钮的有效期一致，按钮有效期内翻页不会再请求音乐平台。
	searchCacheTTL = searchLifetime
	// searchCacheSize 是缓存的关键词数上限，超出时淘汰最早写入的。
	searchCacheSize = 256
)

// searchCache 按 (provider, 关键词) 缓存搜索结果。/search 翻页、面板搜索、关键词点歌、
// inline 搜索与歌词搜索都从这里取，同一关键词在有效期内只请求一次音乐平台。零值可用。
type searchCache struct {
	mu      sync.Mutex
	entries map[searchCacheKey]searchCacheEntry
	group   singleflight.Group
}

type searchCacheKey struct {
	provider string
	keyword  string
}

type searchCacheEntry struct {
	items []provider.TrackSummary
	at    time.Time
}

// searchCacheKeyOf 规范化 provider 与关键词；空 provider 视为默认 provider。
func (a *App) searchCacheKeyOf(providerName, keyword string) searchCacheKey {
	if providerName == "" {
		providerName = a.providers.DefaultName()
	}
	return searchCacheKey{provider: providerName, keyword: strings.Join(strings.Fields(keyword), " ")}
}

func (c *searchCache) get(key searchCacheKey, now time.Time) ([]provider.TrackSummary, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || now.Sub(entry.at) >= searchCacheTTL {
		return nil, false
	}
	return entry.items, true
}

func (c *searchCache) put(key searchCacheKey, items []provider.TrackSummary, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[searchCacheKey]searchCacheEntry)
	}
	if len(c.entries) >= searchCacheSize {
		var oldest searchCacheKey
		var oldestAt time.Time
		for k, e := range c.entries {
			if now.Sub(e.at) >= searchCacheTTL {
				delete(c.entries, k)
			} else if oldestAt.IsZero() || e.at.Before(oldestAt) {
				oldest, oldestAt = k, e.at
			}
		}
		if len(c.entries) >= searchCacheSize {
			delete(c.entries, oldest)
		}
	}
	c.entries[key] = searchCacheEntry{items: items, at: now}
}

// cachedSearch 返回有效期内已缓存的结果，不发起请求。
func (a *App) cachedSearch(providerName, keyword string) ([]provider.TrackSummary, bool) {
	return a.searches.get(a.searchCacheKeyOf(providerName, keyword), time.Now())
}

// searchTracks 返回关键词的前 searchFetchLimit 条结果，优先取缓存；并发的相同搜索只请求一次。
// 返回的切片与缓存共享，调用方不得修改。失败的结果不缓存。
func (a *App) searchTracks(ctx context.Context, providerName, keyword string) ([]provider.TrackSummary, error) {
	key := a.searchCacheKeyOf(providerName, keyword)
	if items, ok := a.searches.get(key, time.Now()); ok {
		return items, nil
	}
	// 共享的请求不随某一个调用方取消；插件调用本身受 [plugins] call_timeout 限制
	shared := context.WithoutCancel(ctx)
	result, err, _ := a.searches.group.Do(key.provider+"\x00"+key.keyword, func() (any, error) {
		items, _, err := a.providers.Search(shared, key.provider, key.keyword, searchFetchLimit, 0)
		if err != nil {
			return nil, err
		}
		items = items[:min(len(items), searchFetchLimit)]
		a.searches.put(key, items, time.Now())
		return items, nil
	})
	if err != nil {
		return nil, err
	}
	return result.([]provider.TrackSummary), nil
}
