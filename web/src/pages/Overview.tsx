import { useQuery } from '@tanstack/react-query'
import { SongCell, songSubtitle } from '../components/SongList'
import { CellSkeleton, Empty, ErrorState, Tile } from '../components/ui'
import { get, type Me, type Overview as OverviewData, type Page, type UserSong } from '../lib/api'
import { formatNumber, formatRelative, splitBytes, splitDuration } from '../lib/format'
import { refKey, useSendSong } from '../lib/hooks'
import { switchTab } from '../lib/router'
import { t } from '../lib/i18n'

export function Overview({ me, hidden }: { me: Me; hidden: boolean }) {
  const overview = useQuery({ queryKey: ['overview'], queryFn: () => get<OverviewData>('/overview') })
  const recent = useQuery({
    queryKey: ['songs', 'recent'],
    queryFn: () => get<Page<UserSong>>('/songs?limit=5'),
  })
  const { send, sending } = useSendSong()
  const data = overview.data

  return (
    <div className="page tab-in" hidden={hidden}>
      <header className="greeting">
        <small>{t.overview.hello}</small>
        <h1 className="truncate">{me.name || t.overview.anonymous}</h1>
      </header>

      {overview.isError ? (
        <ErrorState error={overview.error} onRetry={overview.refetch} />
      ) : (
        <div className="tiles">
          <Tile label={t.overview.songs} parts={data && [[formatNumber(data.songs), t.units.songs]]} />
          <Tile label={t.overview.duration} parts={data && splitDuration(data.duration)} />
          <Tile label={t.overview.size} parts={data && [splitBytes(data.size)]} />
          <Tile label={t.overview.totalCached} parts={data && [[formatNumber(data.total_cached), t.units.songs]]} />
        </div>
      )}

      <h2 className="section-title">
        {t.overview.recent}
        {!!recent.data?.total && <button onClick={() => switchTab('/songs')}>{t.overview.all(recent.data.total)}</button>}
      </h2>
      {recent.isPending ? (
        <CellSkeleton rows={5} />
      ) : recent.isError ? (
        <ErrorState error={recent.error} onRetry={recent.refetch} />
      ) : recent.data.items.length === 0 ? (
        <div className="card">
          <Empty
            icon="music"
            title={t.common.noHistory}
            action={
              <button className="btn small" onClick={() => switchTab('/search')}>
                {t.common.goSearch}
              </button>
            }
          >
            {t.overview.emptyHint}
          </Empty>
        </div>
      ) : (
        <div className="card">
          {recent.data.items.map((song) => {
            const ref = { provider: song.provider, id: song.track_id }
            return (
              <SongCell
                key={song.id}
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
          })}
        </div>
      )}
      {!!recent.data?.items.length && <p className="section-note">{t.overview.note}</p>}
    </div>
  )
}
