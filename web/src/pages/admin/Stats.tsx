import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { BarList, TrendChart } from '../../components/charts'
import { Chips, ErrorState, Segmented, Skeleton } from '../../components/ui'
import { get, query, type Me, type Stats as StatsData } from '../../lib/api'

const SOURCES: Record<string, string> = {
  command: '命令',
  callback: '搜索结果按钮',
  link: '分享链接',
  start: 'Bot内获取',
  guest: 'Guest Mode',
  webapp: '看板',
}

function ChartSkeleton() {
  return (
    <div className="card pad" aria-hidden>
      <Skeleton width={72} height={14} />
      <div style={{ height: 8 }} />
      <Skeleton width={96} height={31} />
      <div style={{ height: 14 }} />
      <Skeleton width="100%" height={132} />
    </div>
  )
}

export function Stats({ me }: { me: Me }) {
  const [days, setDays] = useState(7)
  const [provider, setProvider] = useState('')
  const stats = useQuery({
    queryKey: ['admin', 'stats', days, provider],
    queryFn: () => get<StatsData>('/admin/stats' + query({ days, provider })),
    placeholderData: keepPreviousData,
  })
  const s = stats.data
  const range = `近${days}天`

  return (
    <div className="page sub">
      <h1 className="large-title">统计</h1>
      <div className="sticky-bar">
        <Segmented
          value={days}
          onChange={setDays}
          options={[
            { value: 7, label: '7天' },
            { value: 30, label: '30天' },
            { value: 90, label: '90天' },
          ]}
        />
        {me.providers.length > 1 && (
          <Chips
            value={provider}
            onChange={setProvider}
            options={[{ value: '', label: '全部平台' }, ...me.providers.map((p) => ({ value: p.name, label: p.display }))]}
          />
        )}
      </div>

      {stats.isError && !s ? (
        <ErrorState error={stats.error} onRetry={stats.refetch} />
      ) : !s ? (
        <div className="stack">
          <ChartSkeleton />
          <ChartSkeleton />
        </div>
      ) : (
        <div style={{ opacity: stats.isPlaceholderData ? 0.55 : 1, transition: 'opacity var(--t-fast) ease-out' }}>
          <div className="stack">
            <TrendChart title={`${range}点歌量`} data={s.requests} />
            <TrendChart title={`${range}新增缓存`} data={s.cached} />
          </div>

          <h2 className="section-title">热门歌曲</h2>
          <BarList
            empty="这段时间还没有点歌记录"
            items={s.top_songs.map((song) => ({ key: song.id, label: song.title, sub: song.artists, value: song.count }))}
          />

          <h2 className="section-title">活跃用户</h2>
          <BarList
            empty="这段时间还没有点歌记录"
            items={s.top_users.map((u) => ({
              key: u.id,
              label: u.name || String(u.id),
              sub: u.name ? String(u.id) : undefined,
              value: u.count,
            }))}
          />

          <h2 className="section-title">活跃聊天</h2>
          <BarList
            empty="这段时间还没有点歌记录"
            items={s.top_chats.map((c) => ({
              key: c.id,
              label: c.name || String(c.id),
              sub: c.name ? String(c.id) : undefined,
              value: c.count,
            }))}
          />

          <h2 className="section-title">点歌方式</h2>
          <BarList
            empty="这段时间还没有点歌记录"
            items={s.requests_by_source.map((r) => ({ key: r.key, label: SOURCES[r.key] ?? r.key, value: r.count }))}
          />

          {provider === '' && (
            <>
              <h2 className="section-title">各平台点歌量</h2>
              <BarList
                empty="这段时间还没有点歌记录"
                items={s.requests_by_provider.map((r) => ({ key: r.key, label: r.name, value: r.count }))}
              />
              <h2 className="section-title">各平台缓存总数</h2>
              <BarList items={s.cache_by_provider.map((r) => ({ key: r.key, label: r.name, value: r.count }))} />
            </>
          )}
          <p className="section-note">点歌量从启用点歌记录后开始统计，inline选歌不计入。数据每分钟更新。</p>
        </div>
      )}
    </div>
  )
}
