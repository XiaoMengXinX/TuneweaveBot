import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { BarList, TrendChart } from '../../components/charts'
import { Chips, ErrorState, Segmented, Skeleton } from '../../components/ui'
import { get, query, type Me, type Stats as StatsData } from '../../lib/api'
import { t } from '../../lib/i18n'


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
  
  return (
    <div className="page sub">
      <h1 className="large-title">{t.stats.title}</h1>
      <div className="sticky-bar">
        <Segmented
          value={days}
          onChange={setDays}
          options={[
            { value: 7, label: t.stats.days(7) },
            { value: 30, label: t.stats.days(30) },
            { value: 90, label: t.stats.days(90) },
          ]}
        />
        {me.providers.length > 1 && (
          <Chips
            value={provider}
            onChange={setProvider}
            options={[{ value: '', label: t.common.allPlatforms }, ...me.providers.map((p) => ({ value: p.name, label: p.display }))]}
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
            <TrendChart title={t.stats.requests(days)} data={s.requests} />
            <TrendChart title={t.stats.cached(days)} data={s.cached} />
          </div>

          <h2 className="section-title">{t.stats.topSongs}</h2>
          <BarList
            empty={t.stats.empty}
            items={s.top_songs.map((song) => ({ key: song.id, label: song.title, sub: song.artists, value: song.count }))}
          />

          <h2 className="section-title">{t.stats.topUsers}</h2>
          <BarList
            empty={t.stats.empty}
            items={s.top_users.map((u) => ({
              key: u.id,
              label: u.name || String(u.id),
              sub: u.name ? String(u.id) : undefined,
              value: u.count,
            }))}
          />

          <h2 className="section-title">{t.stats.topChats}</h2>
          <BarList
            empty={t.stats.empty}
            items={s.top_chats.map((c) => ({
              key: c.id,
              label: c.name || String(c.id),
              sub: c.name ? String(c.id) : undefined,
              value: c.count,
            }))}
          />

          <h2 className="section-title">{t.stats.sources}</h2>
          <BarList
            empty={t.stats.empty}
            items={s.requests_by_source.map((r) => ({ key: r.key, label: t.sources[r.key] ?? r.key, value: r.count }))}
          />

          {provider === '' && (
            <>
              <h2 className="section-title">{t.stats.byProvider}</h2>
              <BarList
                empty={t.stats.empty}
                items={s.requests_by_provider.map((r) => ({ key: r.key, label: r.name, value: r.count }))}
              />
              <h2 className="section-title">{t.stats.cacheByProvider}</h2>
              <BarList items={s.cache_by_provider.map((r) => ({ key: r.key, label: r.name, value: r.count }))} />
            </>
          )}
          <p className="section-note">{t.stats.note}</p>
        </div>
      )}
    </div>
  )
}
