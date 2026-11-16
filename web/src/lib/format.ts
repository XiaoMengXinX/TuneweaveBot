import { locale, t } from './i18n'

const number = new Intl.NumberFormat(locale)

export const formatNumber = (n: number) => number.format(n)

/** 字节数拆成数值与单位，便于分开排版 */
export function splitBytes(bytes: number): [string, string] {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  const digits = i === 0 || value >= 100 ? 0 : value >= 10 ? 1 : 2
  return [value.toFixed(digits), units[i]]
}

export const formatBytes = (bytes: number) => splitBytes(bytes).join(' ')

/** 歌曲时长 m:ss */
export function formatTrack(seconds: number): string {
  const m = Math.floor(seconds / 60)
  const s = Math.floor(seconds % 60)
  return `${m}:${String(s).padStart(2, '0')}`
}

/** 较长的时长，取最大的两级单位，如 [["3","天"],["4","小时"]] */
export function splitDuration(seconds: number): [string, string][] {
  const parts: [number, string][] = [
    [Math.floor(seconds / 86400), t.units.day],
    [Math.floor((seconds % 86400) / 3600), t.units.hour],
    [Math.floor((seconds % 3600) / 60), t.units.minute],
  ]
  const first = parts.findIndex(([n]) => n > 0)
  if (first < 0) return [[String(Math.floor(seconds)), t.units.second]]
  return parts
    .slice(first, first + 2)
    .filter(([n], i) => i === 0 || n > 0)
    .map(([n, unit]) => [String(n), unit])
}

const dateFormat = new Intl.DateTimeFormat(locale, { year: 'numeric', month: 'short', day: 'numeric' })
const shortDate = new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric' })
const dateTime = new Intl.DateTimeFormat(locale, {
  year: 'numeric',
  month: 'numeric',
  day: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})
const monthFormat = new Intl.DateTimeFormat(locale, { year: 'numeric', month: 'long' })

export const formatDateTime = (unix: number) => dateTime.format(unix * 1000)

const moment = new Intl.DateTimeFormat(locale, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' })

/** 不带年份的日期时间，如 11月13日 21:07 / Nov 13, 21:07 */
export const formatMoment = (unix: number) => moment.format(unix * 1000)

/** 相对时间，超过一周显示日期 */
export function formatRelative(unix: number, now = Date.now()): string {
  const diff = Math.max(0, now / 1000 - unix)
  if (diff < 60) return t.relative.now
  if (diff < 3600) return t.relative.minutes(Math.floor(diff / 60))
  if (diff < 86400) return t.relative.hours(Math.floor(diff / 3600))
  if (diff < 7 * 86400) return t.relative.days(Math.floor(diff / 86400))
  const date = new Date(unix * 1000)
  return date.getFullYear() === new Date(now).getFullYear() ? shortDate.format(date) : dateFormat.format(date)
}

const fromDay = (day: string) => {
  const [y, m, d] = day.split('-').map(Number)
  return new Date(y, m - 1, d)
}

/** YYYY-MM-DD -> 11月17日 / Nov 17 */
export const formatDay = (day: string) => shortDate.format(fromDay(day))

/** YYYY-MM-DD -> M/D */
export function formatDayShort(day: string): string {
  const [, m, d] = day.split('-')
  return `${Number(m)}/${Number(d)}`
}

/** 年月标题：2026年11月 / November 2026 */
export const formatMonth = (year: number, monthIndex: number) => monthFormat.format(new Date(year, monthIndex, 1))

/** 较短的经过时间，如 "48秒"、"2分05秒" */
export function formatElapsed(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  if (s < 60) return t.elapsed.seconds(s)
  if (s < 3600) return t.elapsed.minutes(Math.floor(s / 60), String(s % 60).padStart(2, '0'))
  return t.elapsed.hours(Math.floor(s / 3600), Math.floor((s % 3600) / 60))
}
