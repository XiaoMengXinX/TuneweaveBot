import { useQuery } from '@tanstack/react-query'
import { Skeleton } from '../../components/ui'
import { get, type DownloadTask, type Downloads } from '../../lib/api'
import { formatBytes, formatElapsed } from '../../lib/format'
import { t } from '../../lib/i18n'

/** 发起下载的聊天：群显示群名，私聊显示用户 */
function chatLabel(task: DownloadTask): string {
  if (task.chat_id < 0) return task.chat_name || String(task.chat_id)
  const name = task.user_name || task.chat_name
  return t.downloads.privateChat(name ? `@${name}` : String(task.chat_id))
}

function TaskRow({ task, now, position }: { task: DownloadTask; now: number; position: number }) {
  const active = task.phase === 'downloading' || task.phase === 'uploading' || task.phase === 'resolving'
  const percent = task.total > 0 ? Math.min(100, (task.written / task.total) * 100) : 0
  const subtitle = [task.artists, task.album].filter(Boolean).join(' - ')
  const since = task.started_at || task.queued_at

  return (
    <div className="task">
      <div className="task-head">
        <span className="cell-title truncate">{task.title || t.downloads.fetching}</span>
        <span className={`badge ${task.phase === 'waiting' ? '' : 'accent'}`}>{t.downloads.phases[task.phase]}</span>
      </div>
      {subtitle && <div className="cell-sub truncate">{subtitle}</div>}
      <div className="task-meta">
        <span className="truncate">
          {/* 发起方与点歌方式之间用中点分隔 */}
          {[chatLabel(task), t.sources[task.source]].filter(Boolean).join(' · ')}
        </span>
        <span className="num">{formatElapsed(now / 1000 - since)}</span>
      </div>
      {task.phase === 'waiting' ? (
        <div className="task-meta">{t.downloads.position(position)}</div>
      ) : (
        active &&
        task.written > 0 && (
          <>
            {/* 大小未知时只显示已传输的大小 */}
            {task.total > 0 && (
              <div className="meter">
                <i style={{ width: `${percent}%` }} />
              </div>
            )}
            <div className="task-meta num">
              <span>
                {formatBytes(task.written)}
                {task.total > 0 && ` / ${formatBytes(task.total)}`}
              </span>
              <span>{task.speed > 0 ? `${formatBytes(task.speed)}/s` : ''}</span>
            </div>
          </>
        )
      )}
    </div>
  )
}

/** 下载并发的实时状态：进行中与排队的任务，含发起的聊天、歌曲与进度 */
export function DownloadPanel({ hidden }: { hidden: boolean }) {
  const downloads = useQuery({
    queryKey: ['admin', 'downloads'],
    queryFn: () => get<Downloads>('/admin/downloads'),
    refetchInterval: hidden ? false : 1500,
  })
  const data = downloads.data
  const running = data?.tasks.filter((t) => t.phase !== 'waiting') ?? []
  const waiting = data?.tasks.filter((t) => t.phase === 'waiting') ?? []
  const now = downloads.dataUpdatedAt || Date.now()

  return (
    <>
      <h2 className="section-title">
        {t.downloads.title}
        {data && (
          <span className="num" style={{ fontWeight: 400 }}>
            {t.downloads.summary(running.length, data.limit, waiting.length)}
          </span>
        )}
      </h2>
      <div className="card">
        {!data ? (
          <div className="task">
            <Skeleton width="50%" />
            <div style={{ height: 8 }} />
            <Skeleton width="100%" height={6} />
          </div>
        ) : data.tasks.length === 0 ? (
          <div className="task task-empty">{t.downloads.empty}</div>
        ) : (
          [...running, ...waiting].map((task) => (
            <TaskRow key={task.id} task={task} now={now} position={waiting.indexOf(task) + 1} />
          ))
        )}
        {data && (
          <div className="meter task-load" aria-label={t.downloads.load(running.length, data.limit)}>
            <i style={{ width: `${(Math.min(running.length, data.limit) / data.limit) * 100}%` }} />
          </div>
        )}
      </div>
    </>
  )
}
