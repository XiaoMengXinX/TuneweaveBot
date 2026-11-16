import { useInfiniteQuery } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { SongCell, songSubtitle } from '../components/SongList'
import { CellSkeleton, Chips, Empty, ErrorState, SearchField } from '../components/ui'
import { get, query, type Me, type SearchPage } from '../lib/api'
import { formatTrack } from '../lib/format'
import { refKey, useSendSong } from '../lib/hooks'

export function Search({ me, hidden }: { me: Me; hidden: boolean }) {
  const [input, setInput] = useState('')
  const [keyword, setKeyword] = useState('')
  const [provider, setProvider] = useState(() => me.providers.find((p) => p.default)?.name ?? '')
  const { send, sending } = useSendSong()
  const sentinel = useRef<HTMLDivElement>(null)

  const results = useInfiniteQuery({
    queryKey: ['search', provider, keyword],
    queryFn: ({ pageParam }) => get<SearchPage>('/search' + query({ q: keyword, provider, offset: pageParam })),
    enabled: keyword !== '',
    initialPageParam: 0,
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.items.length, 0)
      return last.items.length >= last.page_size && loaded < last.total ? loaded : undefined
    },
    staleTime: 5 * 60 * 1000,
    retry: false,
  })
  const items = results.data?.pages.flatMap((p) => p.items) ?? []
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = results

  // 滚到列表末尾时加载下一页
  useEffect(() => {
    const el = sentinel.current
    if (!el || !hasNextPage) return
    const observer = new IntersectionObserver(
      ([entry]) => entry.isIntersecting && !isFetchingNextPage && fetchNextPage(),
      { rootMargin: '200px' },
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [hasNextPage, isFetchingNextPage, fetchNextPage, items.length])

  return (
    <div className="page tab-in" hidden={hidden}>
      <h1 className="large-title">搜索点歌</h1>
      <div className="sticky-bar">
        <SearchField
          value={input}
          onChange={(value) => {
            setInput(value)
            if (value === '') setKeyword('')
          }}
          onSubmit={() => setKeyword(input.trim())}
          placeholder="歌名、歌手"
        />
        {me.providers.length > 1 && (
          <Chips
            value={provider}
            onChange={setProvider}
            options={me.providers.map((p) => ({ value: p.name, label: p.display }))}
          />
        )}
      </div>

      {keyword === '' ? (
        <Empty icon="search" title="搜索想听的歌">
          点击结果，bot会下载并发到你的私聊
        </Empty>
      ) : results.isPending ? (
        <CellSkeleton rows={8} />
      ) : results.isError ? (
        <ErrorState error={results.error} onRetry={results.refetch} />
      ) : items.length === 0 ? (
        <Empty icon="search" title="未找到结果">
          换个关键词或平台试试
        </Empty>
      ) : (
        <>
          <div className="card">
            {items.map((item) => (
              <SongCell
                key={refKey(item)}
                title={item.title}
                subtitle={songSubtitle(item)}
                busy={sending === refKey(item)}
                onClick={() => send({ provider: item.provider, id: item.id, sub: item.sub })}
                side={
                  <>
                    {item.cached && <span className="badge accent">已缓存</span>}
                    {item.duration > 0 && <span className="num">{formatTrack(item.duration)}</span>}
                  </>
                }
              />
            ))}
          </div>
          <div ref={sentinel} style={{ height: 1 }} />
          {isFetchingNextPage && (
            <div style={{ marginTop: 10 }}>
              <CellSkeleton rows={2} />
            </div>
          )}
        </>
      )}
    </div>
  )
}
