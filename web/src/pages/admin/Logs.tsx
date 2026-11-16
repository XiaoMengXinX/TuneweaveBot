import { keepPreviousData, useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Icon } from '../../components/Icon'
import { CellSkeleton, Empty, ErrorState, Segmented } from '../../components/ui'
import { get, query, type LogEntry, type Logs as LogsData } from '../../lib/api'
import { formatDay, formatMonth } from '../../lib/format'
import { haptic } from '../../lib/telegram'
import { t } from '../../lib/i18n'

const LEVELS = [
  { value: '', label: t.logs.all },
  { value: 'debug', label: 'Debug' },
  { value: 'info', label: 'Info' },
  { value: 'warning', label: 'Warn' },
  { value: 'error', label: 'Error' },
]

const BADGE: Record<string, string> = { WARNING: 'warn', ERROR: 'danger', FATAL: 'danger', PANIC: 'danger', INFO: 'accent' }
const LABEL: Record<string, string> = { WARNING: 'WARN' }
const LIVE_INTERVAL = 2000

/** 给每条日志一个不随列表增长而变化的 key：内容相同的条目按出现次序区分 */
function keyed(entries: LogEntry[]): [string, LogEntry][] {
  const seen = new Map<string, number>()
  // 从最旧的一条开始计数，新日志追加在前面时已有条目的序号不变
  return entries
    .toReversed()
    .map((entry): [string, LogEntry] => {
      const id = `${entry.time}|${entry.caller}|${entry.message}`
      const n = seen.get(id) ?? 0
      seen.set(id, n + 1)
      return [`${id}|${n}`, entry]
    })
    .reverse()
}

const pad = (n: number) => String(n).padStart(2, '0')

/** 月历：只有 dates 中的日期（有日志的日期）可选 */
function Calendar({
  value,
  today,
  dates,
  onPick,
}: {
  value: string
  today: string
  dates: string[]
  onPick: (date: string) => void
}) {
  // 以 YYYY-MM 表示当前显示的月份
  const [month, setMonth] = useState(value.slice(0, 7))
  const [year, monthIndex] = [Number(month.slice(0, 4)), Number(month.slice(5)) - 1]
  const available = new Set(dates)
  const firstMonth = dates[dates.length - 1].slice(0, 7)
  const lastMonth = today.slice(0, 7)
  const shift = (offset: number) => {
    const d = new Date(year, monthIndex + offset, 1)
    haptic.select()
    setMonth(`${d.getFullYear()}-${pad(d.getMonth() + 1)}`)
  }
  // 周一为一周的第一天
  const blanks = (new Date(year, monthIndex, 1).getDay() + 6) % 7
  const days = new Date(year, monthIndex + 1, 0).getDate()

  return (
    <div className="card calendar">
      <div className="calendar-head">
        <button aria-label={t.logs.prevMonth} disabled={month <= firstMonth} onClick={() => shift(-1)}>
          <Icon name="chevron" size={18} />
        </button>
        <span>{formatMonth(year, monthIndex)}</span>
        <button aria-label={t.logs.nextMonth} disabled={month >= lastMonth} onClick={() => shift(1)}>
          <Icon name="chevron" size={18} />
        </button>
      </div>
      <div className="calendar-grid">
        {t.logs.weekdays.map((day) => (
          <span className="weekday" key={day}>
            {day}
          </span>
        ))}
        {Array.from({ length: blanks }, (_, i) => (
          <span key={`blank-${i}`} />
        ))}
        {Array.from({ length: days }, (_, i) => {
          const date = `${month}-${pad(i + 1)}`
          return (
            <button
              key={date}
              className={date === today ? 'today' : undefined}
              aria-pressed={date === value}
              disabled={!available.has(date)}
              onClick={() => onPick(date)}
            >
              {i + 1}
            </button>
          )
        })}
      </div>
    </div>
  )
}

function Entry({ entry }: { entry: LogEntry }) {
  const [open, setOpen] = useState(false)
  return (
    <div className={`log ${open ? 'open' : ''}`} onClick={() => setOpen(!open)}>
      <div className="log-head">
        <span className={`badge ${BADGE[entry.level] ?? ''}`}>{LABEL[entry.level] ?? entry.level}</span>
        {/* 页面只显示一天的日志，时间只保留时分秒 */}
        <span className="num">{entry.time.slice(11)}</span>
        <span className="caller mono truncate">{entry.caller}</span>
      </div>
      <div className="log-msg">{entry.message}</div>
    </div>
  )
}

export function Logs() {
  const [level, setLevel] = useState('')
  // null 表示当天，由服务端给出具体日期
  const [date, setDate] = useState<string | null>(null)
  const [live, setLive] = useState(true)
  const [picking, setPicking] = useState(false)

  const logs = useQuery({
    queryKey: ['admin', 'logs', date, level],
    queryFn: () => get<LogsData>('/admin/logs' + query({ date: date ?? undefined, level })),
    // 只有当天的日志还在增长
    refetchInterval: (q) => (live && (date === null || date === q.state.data?.today) ? LIVE_INTERVAL : false),
    placeholderData: keepPreviousData,
  })
  const data = logs.data
  const isToday = !data || data.date === data.today
  const index = data ? data.dates.indexOf(data.date) : -1
  const pick = (next: string) => {
    haptic.select()
    setDate(next === data!.today ? null : next)
    setPicking(false)
  }

  return (
    <div className="page sub">
      <h1 className="large-title">{t.logs.title}</h1>
      <div className="sticky-bar">
        <div className="row">
          <div className="stepper" style={{ flex: 1 }}>
            <button aria-label={t.logs.prevDay} disabled={!data || index < 0 || index >= data.dates.length - 1} onClick={() => pick(data!.dates[index + 1])}>
              <Icon name="chevron" size={18} />
            </button>
            <button
              className="stepper-label"
              aria-label={t.logs.pickDate}
              aria-expanded={picking}
              disabled={!data}
              onClick={() => {
                haptic.select()
                setPicking(!picking)
              }}
            >
              <Icon name="calendar" size={16} />
              {data ? formatDay(data.date) : ''}
              {data && isToday && <small>{t.logs.today}</small>}
            </button>
            <button aria-label={t.logs.nextDay} disabled={!data || index <= 0} onClick={() => pick(data!.dates[index - 1])}>
              <Icon name="chevron" size={18} />
            </button>
          </div>
          <button
            className="icon-btn"
            aria-pressed={live && isToday}
            aria-label={live ? t.logs.stopLive : t.logs.startLive}
            disabled={!isToday}
            onClick={() => {
              haptic.select()
              setLive(!live)
            }}
          >
            <Icon name={live && isToday ? 'stop' : 'play'} size={20} />
          </button>
        </div>
        {picking && data && <Calendar value={data.date} today={data.today} dates={data.dates} onPick={pick} />}
        <Segmented value={level} onChange={setLevel} options={LEVELS} />
      </div>

      {logs.isPending ? (
        <CellSkeleton rows={8} side={false} />
      ) : logs.isError && !data ? (
        <ErrorState error={logs.error} onRetry={logs.refetch} />
      ) : (
        <>
          <p className="list-count" style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
            {live && isToday && <i className="live-dot" />}
            {isToday ? (live ? t.logs.live : t.logs.stopped) : t.logs.newestFirst}
          </p>
          {data!.entries.length === 0 ? (
            <Empty icon="terminal" title={t.logs.empty}>
              {level ? t.logs.emptyLevel : t.logs.emptyDay}
            </Empty>
          ) : (
            <div className="card" style={{ opacity: logs.isPlaceholderData ? 0.55 : 1, transition: 'opacity var(--t-fast) ease-out' }}>
              {keyed(data!.entries).map(([key, entry]) => (
                <Entry key={key} entry={entry} />
              ))}
            </div>
          )}
          {data!.entries.length > 0 && <p className="section-note">{t.logs.expandHint}</p>}
        </>
      )}
    </div>
  )
}
