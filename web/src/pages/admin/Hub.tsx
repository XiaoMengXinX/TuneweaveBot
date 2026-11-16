import { useQuery } from '@tanstack/react-query'
import { Icon } from '../../components/Icon'
import { AuthBadge, ErrorState, NavCell, Skeleton, Tile } from '../../components/ui'
import { get, type Status } from '../../lib/api'
import { formatBytes, formatNumber, splitBytes, splitDuration } from '../../lib/format'
import { push } from '../../lib/router'

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
      <h1 className="large-title">管理</h1>

      {status.isError && !s ? (
        <ErrorState error={status.error} onRetry={status.refetch} />
      ) : (
        <div className="stack">
          {expired.length > 0 && (
            <button className="notice danger" onClick={() => push('/admin/login')}>
              <Icon name="alert" size={18} />
              <span>{expired.map((p) => p.display).join('、')}的登录已失效，新歌下载已暂停。点此重新登录。</span>
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
                {s.commit && s.build_time && '，'}
                {s.build_time && `构建于${s.build_time}`}
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
            <Tile label="运行时长" parts={s && splitDuration(Date.now() / 1000 - s.started_at)} />
            <Tile label="下载并发" parts={s && [[String(s.downloads.active), `/ ${s.downloads.limit}`]]}>
              {s && (
                <>
                  <div className="meter">
                    <i style={{ width: `${(s.downloads.active / s.downloads.limit) * 100}%` }} />
                  </div>
                  <div className="tile-foot">
                    {s.downloads.waiting > 0 ? `${s.downloads.waiting}个任务排队中` : '无排队任务'}
                  </div>
                </>
              )}
            </Tile>
            <Tile label="缓存歌曲" parts={s && [[formatNumber(s.cache_total), '首']]} />
            <Tile label="数据库" parts={s && [splitBytes(s.db_size)]} />
          </div>
        </div>
      )}

      <h2 className="section-title">功能</h2>
      <div className="card">
        <NavCell icon="chart" title="统计" onClick={() => push('/admin/stats')} />
        <NavCell icon="database" title="缓存管理" onClick={() => push('/admin/cache')} />
        <NavCell
          icon="key"
          title="登录"
          hint={expired.length > 0 && <span className="badge danger">{expired.length}个已失效</span>}
          onClick={() => push('/admin/login')}
        />
        <NavCell icon="puzzle" title="插件" onClick={() => push('/admin/plugins')} />
        <NavCell icon="file" title="配置" onClick={() => push('/admin/config')} />
        <NavCell icon="terminal" title="日志" onClick={() => push('/admin/logs')} />
      </div>

      {s && (
        <>
          <h2 className="section-title">平台</h2>
          <div className="card">
            {s.providers.map((p) => (
              <div className="cell" key={p.name}>
                <div className="cell-main">
                  <div className="cell-title">{p.display}</div>
                  <div className="cell-sub mono">{p.name}</div>
                </div>
                <div className="cell-side inline">
                  {p.default && <span className="badge accent">默认</span>}
                  <AuthBadge auth={p.auth} />
                </div>
              </div>
            ))}
          </div>

          <h2 className="section-title">存储</h2>
          <dl className="card">
            <div className="kv">
              <dt>缓存目录</dt>
              <dd className="num">{formatBytes(s.cache_dir_size)}</dd>
            </div>
            <div className="kv">
              <dt>缓存群</dt>
              <dd className="selectable">
                {s.cache_chat.id === 0 ? (
                  '未设置'
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
