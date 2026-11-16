import type { CSSProperties, ReactNode } from 'react'
import { haptic } from '../lib/telegram'
import { Icon, type IconName } from './Icon'
import { t } from '../lib/i18n'

export function Skeleton({ width, height = 16 }: { width: number | string; height?: number }) {
  return <span className="skeleton" style={{ width, height }} />
}

/** 列表骨架，行高与真实列表项一致 */
export function CellSkeleton({ rows = 6, side = true }: { rows?: number; side?: boolean }) {
  return (
    <div className="card" aria-hidden>
      {Array.from({ length: rows }, (_, i) => (
        <div className="cell" key={i}>
          <div className="cell-main">
            <Skeleton width={`${46 + ((i * 17) % 34)}%`} />
            <div style={{ height: 8 }} />
            <Skeleton width={`${28 + ((i * 23) % 30)}%`} height={12} />
          </div>
          {side && <Skeleton width={44} height={20} />}
        </div>
      ))}
    </div>
  )
}

export function Empty({
  icon,
  title,
  children,
  action,
}: {
  icon: IconName
  title: string
  children?: ReactNode
  action?: ReactNode
}) {
  return (
    <div className="empty">
      <Icon name={icon} size={36} />
      <strong>{title}</strong>
      {children && <span>{children}</span>}
      {action}
    </div>
  )
}

export function ErrorState({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  return (
    <Empty
      icon="alert"
      title={t.common.loadFailed}
      action={
        <button className="btn small" onClick={onRetry}>
          {t.common.retry}
        </button>
      }
    >
      {error instanceof Error ? error.message : String(error)}
    </Empty>
  )
}

export function Segmented<T extends string | number>({
  options,
  value,
  onChange,
}: {
  options: { value: T; label: string }[]
  value: T
  onChange: (value: T) => void
}) {
  const index = options.findIndex((o) => o.value === value)
  return (
    <div className="segmented" style={{ '--count': options.length, '--index': index } as CSSProperties}>
      {options.map((o) => (
        <button
          key={o.value}
          aria-pressed={o.value === value}
          onClick={() => {
            if (o.value === value) return
            haptic.select()
            onChange(o.value)
          }}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

export function Chips<T extends string>({
  options,
  value,
  onChange,
}: {
  options: { value: T; label: string }[]
  value: T
  onChange: (value: T) => void
}) {
  return (
    <div className="chips">
      {options.map((o) => (
        <button
          key={o.value}
          className="chip"
          aria-pressed={o.value === value}
          onClick={() => {
            if (o.value === value) return
            haptic.select()
            onChange(o.value)
          }}
        >
          {o.label}
        </button>
      ))}
    </div>
  )
}

export function SearchField({
  value,
  onChange,
  onSubmit,
  placeholder,
}: {
  value: string
  onChange: (value: string) => void
  onSubmit?: () => void
  placeholder: string
}) {
  return (
    <form
      className="field"
      role="search"
      onSubmit={(e) => {
        e.preventDefault()
        ;(document.activeElement as HTMLElement | null)?.blur()
        onSubmit?.()
      }}
    >
      <Icon name="search" size={18} />
      <input
        type="search"
        enterKeyHint="search"
        autoComplete="off"
        autoCorrect="off"
        spellCheck={false}
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
      {value && (
        <button type="button" className="field-clear" aria-label={t.common.clear} onClick={() => onChange('')}>
          <Icon name="close" size={16} />
        </button>
      )}
    </form>
  )
}

/** 数据卡片：parts 为 [数值, 单位] 序列 */
export function Tile({
  label,
  parts,
  loading,
  children,
}: {
  label: string
  parts?: [string, string?][]
  loading?: boolean
  children?: ReactNode
}) {
  return (
    <div className="card tile">
      <div className="tile-label">{label}</div>
      <div className="tile-value">
        {loading || !parts ? (
          <Skeleton width="60%" height={31} />
        ) : (
          parts.map(([value, unit], i) => (
            <span key={i}>
              {value}
              {unit && <span className="unit">{unit}</span>}
            </span>
          ))
        )}
      </div>
      {children}
    </div>
  )
}

export function AuthBadge({ auth }: { auth: 'ok' | 'expired' | 'none' }) {
  if (auth === 'ok')
    return (
      <span className="badge ok">
        <i className="dot" />
        {t.common.loggedIn}
      </span>
    )
  if (auth === 'expired')
    return (
      <span className="badge danger">
        <i className="dot" />
        {t.common.expired}
      </span>
    )
  return <span className="badge">{t.common.noLogin}</span>
}

export function NavCell({
  icon,
  title,
  hint,
  onClick,
}: {
  icon: IconName
  title: string
  hint?: ReactNode
  onClick: () => void
}) {
  return (
    <button className="cell" onClick={onClick}>
      <span className="cell-icon">
        <Icon name={icon} size={18} />
      </span>
      <span className="cell-main cell-title">{title}</span>
      <span className="cell-side inline">
        {hint}
        <span className="chevron">
          <Icon name="chevron" size={18} />
        </span>
      </span>
    </button>
  )
}
