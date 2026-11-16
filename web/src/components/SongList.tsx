import type { InfiniteData, UseInfiniteQueryResult } from '@tanstack/react-query'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useEffect, useLayoutEffect, useRef, useState, type ReactNode, type RefObject } from 'react'
import type { Page, Song } from '../lib/api'
import { Skeleton } from './ui'

export const PAGE_SIZE = 100
const ROW_HEIGHT = 64
const PENDING_ROWS = 6

export function songSubtitle(song: { artists: string; album: string }): string {
  return song.album ? `${song.artists} - ${song.album}` : song.artists
}

export function SongCell({
  title,
  subtitle,
  side,
  busy,
  onClick,
}: {
  title: string
  subtitle: string
  side?: ReactNode
  busy?: boolean
  onClick: () => void
}) {
  return (
    <button className="cell" style={{ height: ROW_HEIGHT }} onClick={onClick}>
      <span className="cell-main">
        <span className="cell-title truncate" style={{ display: 'block' }}>
          {title}
        </span>
        <span className="cell-sub truncate" style={{ display: 'block' }}>
          {subtitle}
        </span>
      </span>
      <span className="cell-side">{busy ? <span className="spinner" /> : side}</span>
    </button>
  )
}

function PendingCell() {
  return (
    <div className="cell" style={{ height: ROW_HEIGHT }} aria-hidden>
      <div className="cell-main">
        <Skeleton width="52%" />
        <div style={{ height: 8 }} />
        <Skeleton width="34%" height={12} />
      </div>
      <Skeleton width={44} height={20} />
    </div>
  )
}

/** 虚拟滚动的分页歌曲列表：滚到未加载的行时拉取下一页，未加载的行显示骨架 */
export function VirtualSongList<T extends Song>({
  query,
  scrollRef,
  renderRow,
}: {
  query: UseInfiniteQueryResult<InfiniteData<Page<T>>>
  scrollRef: RefObject<HTMLElement | null>
  renderRow: (song: T) => ReactNode
}) {
  const listRef = useRef<HTMLDivElement>(null)
  const [scrollMargin, setScrollMargin] = useState(0)
  const items = query.data?.pages.flatMap((p) => p.items) ?? []
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = query

  useLayoutEffect(() => {
    setScrollMargin(listRef.current?.offsetTop ?? 0)
  }, [items.length === 0])

  const virtualizer = useVirtualizer({
    count: items.length + (hasNextPage ? PENDING_ROWS : 0),
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    overscan: 8,
    scrollMargin,
  })
  const rows = virtualizer.getVirtualItems()
  const lastIndex = rows.at(-1)?.index ?? 0

  useEffect(() => {
    if (lastIndex >= items.length - 1 && hasNextPage && !isFetchingNextPage) fetchNextPage()
  }, [lastIndex, items.length, hasNextPage, isFetchingNextPage, fetchNextPage])

  return (
    <div className="card">
      <div ref={listRef} className="vlist" style={{ height: virtualizer.getTotalSize() }}>
        {rows.map((row) => (
          <div
            key={row.key}
            className="vrow"
            style={{ transform: `translateY(${row.start - scrollMargin}px)`, height: ROW_HEIGHT }}
          >
            {row.index < items.length ? renderRow(items[row.index]) : <PendingCell />}
          </div>
        ))}
      </div>
    </div>
  )
}
