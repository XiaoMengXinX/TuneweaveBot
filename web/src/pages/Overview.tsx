import { useQuery } from '@tanstack/react-query'
import { SongCell, songSubtitle } from '../components/SongList'
import { CellSkeleton, Empty, ErrorState, Tile } from '../components/ui'
import { get, type Me, type Overview as OverviewData, type Page, type UserSong } from '../lib/api'
import { formatNumber, formatRelative, splitBytes, splitDuration } from '../lib/format'
import { refKey, useSendSong } from '../lib/hooks'
import { switchTab } from '../lib/router'

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
        <small>你好</small>
        <h1 className="truncate">{me.name || 'Telegram用户'}</h1>
      </header>

      {overview.isError ? (
        <ErrorState error={overview.error} onRetry={overview.refetch} />
      ) : (
        <div className="tiles">
          <Tile label="点过的歌曲" parts={data && [[formatNumber(data.songs), '首']]} />
          <Tile label="总时长" parts={data && splitDuration(data.duration)} />
          <Tile label="总大小" parts={data && [splitBytes(data.size)]} />
          <Tile label="全站缓存" parts={data && [[formatNumber(data.total_cached), '首']]} />
        </div>
      )}

      <h2 className="section-title">
        最近点过
        {!!recent.data?.total && <button onClick={() => switchTab('/songs')}>全部{recent.data.total}首</button>}
      </h2>
      {recent.isPending ? (
        <CellSkeleton rows={5} />
      ) : recent.isError ? (
        <ErrorState error={recent.error} onRetry={recent.refetch} />
      ) : recent.data.items.length === 0 ? (
        <div className="card">
          <Empty
            icon="music"
            title="还没有点歌记录"
            action={
              <button className="btn small" onClick={() => switchTab('/search')}>
                去搜索
              </button>
            }
          >
            搜索一首歌，或把分享链接发给bot
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
      {!!recent.data?.items.length && <p className="section-note">点一首歌，bot会把它发到你的私聊。</p>}
    </div>
  )
}
