import { keepPreviousData, useInfiniteQuery } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { Icon } from '../components/Icon'
import { PAGE_SIZE, SongCell, songSubtitle, VirtualSongList } from '../components/SongList'
import { CellSkeleton, Empty, ErrorState, SearchField } from '../components/ui'
import { get, query, type Page, type UserSong } from '../lib/api'
import { formatNumber, formatRelative } from '../lib/format'
import { refKey, useDebounced, useSendSong } from '../lib/hooks'
import { switchTab } from '../lib/router'
import { haptic } from '../lib/telegram'

export function Songs({ hidden }: { hidden: boolean }) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const [keyword, setKeyword] = useState('')
  const [oldestFirst, setOldestFirst] = useState(false)
  const q = useDebounced(keyword.trim())
  const { send, sending } = useSendSong()

  const songs = useInfiniteQuery({
    queryKey: ['songs', 'list', q, oldestFirst],
    queryFn: ({ pageParam }) =>
      get<Page<UserSong>>(
        '/songs' + query({ q, order: oldestFirst ? 'asc' : 'desc', limit: PAGE_SIZE, offset: pageParam }),
      ),
    initialPageParam: 0,
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.items.length, 0)
      return last.items.length > 0 && loaded < last.total ? loaded : undefined
    },
    placeholderData: keepPreviousData,
  })
  const total = songs.data?.pages[0]?.total ?? 0

  return (
    <div className="page tab-in" hidden={hidden} ref={scrollRef}>
      <h1 className="large-title">我的歌曲</h1>
      <div className="sticky-bar">
        <div className="row">
          <SearchField value={keyword} onChange={setKeyword} placeholder="搜索歌名、歌手或专辑" />
          <button
            className={`icon-btn ${oldestFirst ? 'flipped' : ''}`}
            aria-label={oldestFirst ? '当前最早优先，切换为最近优先' : '当前最近优先，切换为最早优先'}
            onClick={() => {
              haptic.select()
              setOldestFirst(!oldestFirst)
              scrollRef.current?.scrollTo({ top: 0 })
            }}
          >
            <Icon name="sort" size={20} />
          </button>
        </div>
      </div>

      {songs.isPending ? (
        <CellSkeleton rows={8} />
      ) : songs.isError ? (
        <ErrorState error={songs.error} onRetry={songs.refetch} />
      ) : total === 0 ? (
        q ? (
          <Empty icon="search" title="没有匹配的歌曲">
            换个关键词试试
          </Empty>
        ) : (
          <Empty
            icon="music"
            title="还没有点歌记录"
            action={
              <button className="btn small" onClick={() => switchTab('/search')}>
                去搜索
              </button>
            }
          >
            点过的歌会出现在这里
          </Empty>
        )
      ) : (
        <>
          <p className="list-count">
            共{formatNumber(total)}首，{oldestFirst ? '最早点的在前' : '最近点的在前'}
          </p>
          <VirtualSongList
            query={songs}
            scrollRef={scrollRef}
            renderRow={(song) => {
              const ref = { provider: song.provider, id: song.track_id }
              return (
                <SongCell
                  title={song.title}
                  subtitle={songSubtitle(song)}
                  busy={sending === refKey(ref)}
                  onClick={() => send(ref)}
                  side={
                    <>
                      <span className="badge">{song.provider_name}</span>
                      <span>{formatRelative(song.last_at)}</span>
                    </>
                  }
                />
              )
            }}
          />
        </>
      )}
    </div>
  )
}
