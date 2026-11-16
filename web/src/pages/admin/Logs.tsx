import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Icon } from '../../components/Icon'
import { CellSkeleton, Empty, ErrorState, Segmented } from '../../components/ui'
import { get, query, type LogEntry } from '../../lib/api'

const LEVELS = [
  { value: '', label: '全部' },
  { value: 'debug', label: 'Debug' },
  { value: 'info', label: 'Info' },
  { value: 'warning', label: 'Warn' },
  { value: 'error', label: 'Error' },
]

const BADGE: Record<string, string> = { WARNING: 'warn', ERROR: 'danger', FATAL: 'danger', PANIC: 'danger', INFO: 'accent' }
const LABEL: Record<string, string> = { WARNING: 'WARN' }

function Entry({ entry }: { entry: LogEntry }) {
  const [open, setOpen] = useState(false)
  return (
    <div className={`log ${open ? 'open' : ''}`} onClick={() => setOpen(!open)}>
      <div className="log-head">
        <span className={`badge ${BADGE[entry.level] ?? ''}`}>{LABEL[entry.level] ?? entry.level}</span>
        {/* 去掉年份：2006/01/02 15:04:05 -> 01/02 15:04:05 */}
        <span className="num">{entry.time.slice(5)}</span>
        <span className="caller mono truncate">{entry.caller}</span>
      </div>
      <div className="log-msg">{entry.message}</div>
    </div>
  )
}

export function Logs() {
  const [level, setLevel] = useState('')
  const logs = useQuery({
    queryKey: ['admin', 'logs', level],
    queryFn: () => get<{ entries: LogEntry[] }>('/admin/logs' + query({ level })),
    refetchInterval: 5000,
    placeholderData: keepPreviousData,
  })

  return (
    <div className="page sub">
      <h1 className="large-title">日志</h1>
      <div className="sticky-bar">
        <div className="row">
          <div style={{ flex: 1 }}>
            <Segmented value={level} onChange={setLevel} options={LEVELS} />
          </div>
          <button className="icon-btn" aria-label="刷新" onClick={() => logs.refetch()}>
            <Icon name="refresh" size={20} />
          </button>
        </div>
      </div>
      {logs.isPending ? (
        <CellSkeleton rows={8} side={false} />
      ) : logs.isError && !logs.data ? (
        <ErrorState error={logs.error} onRetry={logs.refetch} />
      ) : logs.data!.entries.length === 0 ? (
        <Empty icon="terminal" title="没有日志">
          {level ? '最近的日志里没有这个级别的记录' : '日志目录为空'}
        </Empty>
      ) : (
        <>
          <div className="card">
            {logs.data!.entries.map((entry, i) => (
              <Entry key={`${entry.time}-${logs.data!.entries.length - i}`} entry={entry} />
            ))}
          </div>
          <p className="section-note">最新的在前，每5秒自动刷新。点击一条日志可展开全文。</p>
        </>
      )}
    </div>
  )
}
