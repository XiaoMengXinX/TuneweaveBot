import { useQuery } from '@tanstack/react-query'
import { Icon } from '../../components/Icon'
import { AuthBadge, ErrorState, NavCell, Skeleton, Tile } from '../../components/ui'
import { get, type Status } from '../../lib/api'
import { formatBytes, formatMoment, formatNumber, splitBytes, splitDuration } from '../../lib/format'
import { push } from '../../lib/router'
import { DownloadPanel } from './Downloads'
import { t } from '../../lib/i18n'

export function Hub({ hidden }: { hidden: boolean }) {
  const status = useQuery({
    queryKey: ['admin', 'status'],
    queryFn: () => get<Status>('/admin/status'),
    refetchInterval: hidden ? false : 10_000,
  })
  const s = status.data
  const expired = s?.providers.filter((p) => p.auth === 'expired') ?? []

  return (
    <div className="page tab-in" hidden={hidden}>
      <h1 className="large-title">{t.hub.title}</h1>

      {status.isError && !s ? (
        <ErrorState error={status.error} onRetry={status.refetch} />
      ) : (
        <div className="stack">
          {expired.length > 0 && (
            <button className="notice danger" onClick={() => push('/admin/login')}>
              <Icon name="alert" size={18} />
              <span>{t.hub.expired(expired.map((p) => p.display))}</span>
            </button>
          )}

          <div className="card version-card">
            <h2>
              TuneweaveBot
              {s ? <span className="badge accent">{s.version}</span> : <Skeleton width={56} height={20} />}
            </h2>
            {s ? (
              <p className="selectable">
                {s.commit && <span className="mono">{s.commit.slice(0, 7)}</span>}
                {s.commit && s.build_time && t.common.comma}
                {s.build_time && t.hub.builtAt(s.build_time)}
                {(s.commit || s.build_time) && <br />}
                {s.runtime} {s.arch}
              </p>
            ) : (
              <p>
                <Skeleton width="70%" height={12} />
              </p>
            )}
          </div>

          <div className="tiles">
            <Tile label={t.hub.uptime} parts={s && splitDuration(Date.now() / 1000 - s.started_at)}>
              {s && <div className="tile-foot num">{t.hub.startedAt(formatMoment(s.started_at))}</div>}
            </Tile>
            <Tile label={t.hub.memory} parts={s && [splitBytes(s.memory.sys)]}>
              {s && <div className="tile-foot num">{t.hub.heap(formatBytes(s.memory.heap))}</div>}
            </Tile>
            <Tile label={t.hub.cached} parts={s && [[formatNumber(s.cache_total), t.units.songs]]}>
              {s && <div className="tile-foot num">{t.hub.cachedSize(formatBytes(s.cache_size))}</div>}
            </Tile>
            <Tile label={t.hub.database} parts={s && [splitBytes(s.db_size)]} />
          </div>
        </div>
      )}

      <DownloadPanel hidden={hidden} />

      <h2 className="section-title">{t.hub.features}</h2>
      <div className="card">
        <NavCell icon="chart" title={t.hub.stats} onClick={() => push('/admin/stats')} />
        <NavCell icon="database" title={t.hub.cache} onClick={() => push('/admin/cache')} />
        <NavCell
          icon="key"
          title={t.hub.login}
          hint={expired.length > 0 && <span className="badge danger">{t.hub.expiredCount(expired.length)}</span>}
          onClick={() => push('/admin/login')}
        />
        <NavCell icon="puzzle" title={t.hub.plugins} onClick={() => push('/admin/plugins')} />
        <NavCell icon="file" title={t.hub.config} onClick={() => push('/admin/config')} />
        <NavCell icon="terminal" title={t.hub.logs} onClick={() => push('/admin/logs')} />
      </div>

      {s && (
        <>
          <h2 className="section-title">{t.hub.platforms}</h2>
          <div className="card">
            {s.providers.map((p) => (
              <div className="cell" key={p.name}>
                <div className="cell-main">
                  <div className="cell-title">{p.display}</div>
                  <div className="cell-sub mono">{p.name}</div>
                </div>
                <div className="cell-side inline">
                  {p.default && <span className="badge accent">{t.common.default}</span>}
                  <AuthBadge auth={p.auth} />
                </div>
              </div>
            ))}
          </div>

          <h2 className="section-title">{t.hub.storage}</h2>
          <dl className="card">
            <div className="kv">
              <dt>{t.hub.cacheDir}</dt>
              <dd className="num">{formatBytes(s.cache_dir_size)}</dd>
            </div>
            <div className="kv">
              <dt>{t.hub.cacheChat}</dt>
              <dd className="selectable">
                {s.cache_chat.id === 0 ? (
                  t.common.notSet
                ) : (
                  <>
                    {s.cache_chat.name && `${s.cache_chat.name} `}
                    <span className="mono">{s.cache_chat.id}</span>
                  </>
                )}
              </dd>
            </div>
          </dl>
        </>
      )}
    </div>
  )
}
