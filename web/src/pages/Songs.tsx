import { keepPreviousData, useInfiniteQuery } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { PAGE_SIZE, SongCell, songSubtitle, VirtualSongList } from '../components/SongList'
import { CellSkeleton, Empty, ErrorState, SearchField, Segmented } from '../components/ui'
import { get, query, type Page, type SongOrder, type UserSong } from '../lib/api'
import { formatNumber, formatRelative } from '../lib/format'
import { refKey, useDebounced, useSendSong } from '../lib/hooks'
import { switchTab } from '../lib/router'
import { t } from '../lib/i18n'

const ORDERS: SongOrder[] = ['recent', 'oldest', 'az', 'za']

export function Songs({ hidden }: { hidden: boolean }) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const [keyword, setKeyword] = useState('')
  const [order, setOrder] = useState<SongOrder>('recent')
  const q = useDebounced(keyword.trim())
  const { send, sending } = useSendSong()

  const songs = useInfiniteQuery({
    queryKey: ['songs', 'list', q, order],
    queryFn: ({ pageParam }) =>
      get<Page<UserSong>>(
        '/songs' + query({ q, order, limit: PAGE_SIZE, offset: pageParam }),
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
      <h1 className="large-title">{t.songs.title}</h1>
      <div className="sticky-bar">
        <SearchField value={keyword} onChange={setKeyword} placeholder={t.songs.placeholder} />
        <Segmented
          value={order}
          onChange={(next) => {
            setOrder(next)
            scrollRef.current?.scrollTo({ top: 0 })
          }}
          options={ORDERS.map((value) => ({ value, label: t.songs.orders[value] }))}
        />
      </div>

      {songs.isPending ? (
        <CellSkeleton rows={8} />
      ) : songs.isError ? (
        <ErrorState error={songs.error} onRetry={songs.refetch} />
      ) : total === 0 ? (
        q ? (
          <Empty icon="search" title={t.songs.noMatch}>
            {t.songs.tryAnother}
          </Empty>
        ) : (
          <Empty
            icon="music"
            title={t.common.noHistory}
            action={
              <button className="btn small" onClick={() => switchTab('/search')}>
                {t.common.goSearch}
              </button>
            }
          >
            {t.songs.emptyHint}
          </Empty>
        )
      ) : (
        <>
          <p className="list-count">
            {/* 数量与排序说明之间用中点分隔 */}
            {t.songs.count(formatNumber(total))} · {t.songs.orderNotes[order]}
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
