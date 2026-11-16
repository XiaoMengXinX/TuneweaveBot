import { useLayoutEffect, useRef, useState, type PointerEvent, type ReactNode } from 'react'
import type { DayCount } from '../lib/api'
import { formatDay, formatDayShort, formatNumber } from '../lib/format'
import { haptic } from '../lib/telegram'
import { t } from '../lib/i18n'

const HEIGHT = 132
const PAD = { top: 8, right: 4, bottom: 22, left: 4 }

/** 把最大值取整到便于阅读的刻度上限 */
function niceMax(max: number): number {
  if (max <= 4) return 4
  const pow = 10 ** Math.floor(Math.log10(max))
  for (const step of [1, 2, 4, 5, 10]) {
    if (max <= step * pow) return step * pow
  }
  return 10 * pow
}

/** 单序列趋势折线图：标题即图例，按住或滑动查看某一天的数值 */
export function TrendChart({ title, data }: { title: string; data: DayCount[] }) {
  const box = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  const [active, setActive] = useState<number | null>(null)

  useLayoutEffect(() => {
    const el = box.current!
    const observer = new ResizeObserver(() => setWidth(el.clientWidth))
    observer.observe(el)
    return () => observer.disconnect()
  }, [])

  const total = data.reduce((sum, d) => sum + d.count, 0)
  const top = niceMax(Math.max(...data.map((d) => d.count)))
  const innerW = width - PAD.left - PAD.right
  const innerH = HEIGHT - PAD.top - PAD.bottom
  const x = (i: number) => PAD.left + (data.length > 1 ? (i / (data.length - 1)) * innerW : innerW / 2)
  const y = (v: number) => PAD.top + innerH - (v / top) * innerH
  const points = data.map((d, i) => `${x(i).toFixed(1)},${y(d.count).toFixed(1)}`).join(' ')
  const baseline = PAD.top + innerH

  const pick = (e: PointerEvent) => {
    const rect = box.current!.getBoundingClientRect()
    const ratio = (e.clientX - rect.left - PAD.left) / innerW
    const index = Math.max(0, Math.min(data.length - 1, Math.round(ratio * (data.length - 1))))
    if (index !== active) {
      haptic.select()
      setActive(index)
    }
  }

  const shown = active !== null ? data[active] : null
  // 横轴只标首、中、尾三个日期
  const ticks = [...new Set([0, Math.floor((data.length - 1) / 2), data.length - 1])]

  return (
    <div className="card pad">
      <div className="chart-head">
        <div>
          <div className="chart-title">{title}</div>
          <div className="chart-total">{formatNumber(total)}</div>
        </div>
        <div className="chart-readout" aria-live="polite">
          {shown ? (
            <>
              {formatDay(shown.day)}
              <b>{formatNumber(shown.count)}</b>
            </>
          ) : (
            <>
              {t.chart.dailyAverage}
              <b>{formatNumber(Math.round((total / data.length) * 10) / 10)}</b>
            </>
          )}
        </div>
      </div>
      <div
        ref={box}
        className="line-chart"
        onPointerDown={pick}
        onPointerMove={pick}
        onPointerLeave={() => setActive(null)}
        onPointerCancel={() => setActive(null)}
        onPointerUp={(e) => e.pointerType !== 'mouse' && setActive(null)}
      >
        {width > 0 && (
          <svg width={width} height={HEIGHT} role="img" aria-label={t.chart.label(title, total, data.length)}>
            {[0, 0.5, 1].map((f) => (
              <line key={f} className="grid" x1={PAD.left} x2={width - PAD.right} y1={y(top * f)} y2={y(top * f)} />
            ))}
            <text x={width - PAD.right} y={y(top) + 12} textAnchor="end">
              {formatNumber(top)}
            </text>
            <polygon className="area" points={`${x(0)},${baseline} ${points} ${x(data.length - 1)},${baseline}`} />
            <polyline className="line" points={points} />
            {ticks.map((i) => (
              <text
                key={i}
                x={x(i)}
                y={HEIGHT - 4}
                textAnchor={i === 0 ? 'start' : i === data.length - 1 ? 'end' : 'middle'}
              >
                {formatDayShort(data[i].day)}
              </text>
            ))}
            {active !== null && (
              <>
                <line className="cursor" x1={x(active)} x2={x(active)} y1={PAD.top} y2={baseline} />
                <circle className="marker" cx={x(active)} cy={y(data[active].count)} r={5} />
              </>
            )}
          </svg>
        )}
      </div>
    </div>
  )
}

export interface BarItem {
  key: string | number
  label: ReactNode
  sub?: string
  value: number
}

/** 横向条形图：数值直接标在行尾，条长按最大值归一 */
export function BarList({ items, empty = t.common.noData }: { items: BarItem[]; empty?: string }) {
  if (items.length === 0) {
    return (
      <div className="card pad" style={{ color: 'var(--text-2)', fontSize: 'var(--fs-small)' }}>
        {empty}
      </div>
    )
  }
  const max = Math.max(...items.map((i) => i.value))
  return (
    <div className="card pad bars">
      {items.map((item) => (
        <div key={item.key}>
          <div className="bar-row-head">
            <span className="bar-row-label truncate">
              {item.label}
              {item.sub && <small>{item.sub}</small>}
            </span>
            <span className="bar-row-value">{formatNumber(item.value)}</span>
          </div>
          <div className="bar-track">
            <div className="bar-fill" style={{ width: `${(item.value / max) * 100}%` }} />
          </div>
        </div>
      ))}
    </div>
  )
}
