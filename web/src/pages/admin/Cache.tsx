import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { Icon } from '../../components/Icon'
import { PAGE_SIZE, SongCell, songSubtitle, VirtualSongList } from '../../components/SongList'
import { toast } from '../../components/Toast'
import { CellSkeleton, Chips, Empty, ErrorState, SearchField, Skeleton } from '../../components/ui'
import { del, get, query, type CacheDetail as Detail, type Me, type Page, type Song } from '../../lib/api'
import { formatBytes, formatDateTime, formatNumber, formatRelative, formatTrack } from '../../lib/format'
import { useDebounced, useSendSong } from '../../lib/hooks'
import { back, push } from '../../lib/router'
import { confirmDialog, haptic, openLink } from '../../lib/telegram'
import { t } from '../../lib/i18n'

export function Cache({ me, hidden }: { me: Me; hidden: boolean }) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const [keyword, setKeyword] = useState('')
  const [provider, setProvider] = useState('')
  const q = useDebounced(keyword.trim())

  const songs = useInfiniteQuery({
    queryKey: ['admin', 'cache', 'list', q, provider],
    queryFn: ({ pageParam }) =>
      get<Page<Song>>('/admin/cache' + query({ q, provider, limit: PAGE_SIZE, offset: pageParam })),
    initialPageParam: 0,
    getNextPageParam: (last, pages) => {
      const loaded = pages.reduce((n, p) => n + p.items.length, 0)
      return last.items.length > 0 && loaded < last.total ? loaded : undefined
    },
    placeholderData: keepPreviousData,
  })
  const total = songs.data?.pages[0]?.total ?? 0

  return (
    <div className="page sub" hidden={hidden} ref={scrollRef}>
      <h1 className="large-title">{t.cache.title}</h1>
      <div className="sticky-bar">
        <SearchField value={keyword} onChange={setKeyword} placeholder={t.cache.placeholder} />
        {me.providers.length > 1 && (
          <Chips
            value={provider}
            onChange={setProvider}
            options={[{ value: '', label: t.common.allPlatforms }, ...me.providers.map((p) => ({ value: p.name, label: p.display }))]}
          />
        )}
      </div>

      {songs.isPending ? (
        <CellSkeleton rows={8} />
      ) : songs.isError ? (
        <ErrorState error={songs.error} onRetry={songs.refetch} />
      ) : total === 0 ? (
        <Empty icon="database" title={q || provider ? t.cache.noMatch : t.cache.empty} />
      ) : (
        <>
          <p className="list-count">{t.cache.count(formatNumber(total))}</p>
          <VirtualSongList
            query={songs}
            scrollRef={scrollRef}
            renderRow={(song) => (
              <SongCell
                title={song.title}
                subtitle={songSubtitle(song)}
                onClick={() => {
                  haptic.tap()
                  push(`/admin/cache/${song.id}`)
                }}
                side={
                  <>
                    <span className="badge">{song.provider_name}</span>
                    <span>{formatRelative(song.cached_at)}</span>
                  </>
                }
              />
            )}
          />
        </>
      )}
    </div>
  )
}

export function CacheDetail({ id }: { id: string }) {
  const client = useQueryClient()
  const detail = useQuery({
    queryKey: ['admin', 'cache', 'detail', id],
    queryFn: () => get<Detail>(`/admin/cache/${id}`),
  })
  const { send, sending } = useSendSong()
  const remove = useMutation({
    mutationFn: () => del(`/admin/cache/${id}`),
    onSuccess: () => {
      toast.ok(t.cache.deleted)
      client.invalidateQueries({ queryKey: ['admin', 'cache', 'list'] })
      client.invalidateQueries({ queryKey: ['admin', 'status'] })
      back()
    },
    onError: (error) => toast.error(error),
  })
  const d = detail.data

  if (detail.isError) {
    return (
      <div className="page sub">
        <ErrorState error={detail.error} onRetry={detail.refetch} />
      </div>
    )
  }

  const who = (name: string, id: number) => (name ? `${name} (${id})` : id ? String(id) : t.common.unknown)

  return (
    <div className="page sub">
      <h1 className="large-title">{t.cache.detail}</h1>
      <div className="stack">
        <div className="card song-hero">
          {d ? (
            <>
              <h2 className="selectable">{d.song.title}</h2>
              <p className="selectable">{songSubtitle(d.song)}</p>
              <div className="badges">
                <span className="badge accent">{d.song.provider_name}</span>
                <span className="badge">{d.song.ext.toUpperCase()}</span>
                {d.song.bit_rate > 0 && <span className="badge num">{Math.round(d.song.bit_rate / 1000)} kbps</span>}
                <span className="badge num">{formatTrack(d.song.duration)}</span>
                <span className="badge num">{formatBytes(d.song.size)}</span>
              </div>
            </>
          ) : (
            <>
              <Skeleton width="60%" height={20} />
              <div style={{ height: 10 }} />
              <Skeleton width="40%" height={14} />
              <div style={{ height: 12 }} />
              <Skeleton width="80%" height={20} />
            </>
          )}
        </div>

        {d && (
          <>
            <dl className="card">
              <div className="kv">
                <dt>{t.cache.trackId}</dt>
                <dd className="mono selectable">{d.song.track_id}</dd>
              </div>
              <div className="kv">
                <dt>{t.cache.requests}</dt>
                <dd className="num">{formatNumber(d.requests)}</dd>
              </div>
              <div className="kv">
                <dt>{t.cache.firstCached}</dt>
                <dd className="num">{formatDateTime(d.song.cached_at)}</dd>
              </div>
              <div className="kv">
                <dt>{t.cache.updated}</dt>
                <dd className="num">{formatDateTime(d.updated_at)}</dd>
              </div>
              <div className="kv">
                <dt>{t.cache.cachedBy}</dt>
                <dd className="selectable">{who(d.from_user_name, d.from_user_id)}</dd>
              </div>
              <div className="kv">
                <dt>{t.cache.fromChat}</dt>
                <dd className="selectable">{who(d.from_chat_name, d.from_chat_id)}</dd>
              </div>
            </dl>

            <div className="btn-row">
              <button
                className="btn"
                disabled={sending !== null}
                onClick={() => send({ provider: d.song.provider, id: d.song.track_id })}
              >
                <Icon name="send" size={18} />
                {t.cache.sendToMe}
              </button>
              {d.page_url && (
                <button className="btn" onClick={() => openLink(d.page_url)}>
                  <Icon name="external" size={18} />
                  {t.cache.songPage}
                </button>
              )}
            </div>
            <button
              className="btn danger"
              disabled={remove.isPending}
              onClick={async () => {
                haptic.tap()
                if (await confirmDialog(t.cache.confirmDelete(d.song.title))) remove.mutate()
              }}
            >
              {remove.isPending ? <span className="spinner" /> : <Icon name="trash" size={18} />}
              {t.cache.delete}
            </button>
          </>
        )}
      </div>
    </div>
  )
}
