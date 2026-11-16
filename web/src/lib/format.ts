const number = new Intl.NumberFormat('zh-CN')

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
    [Math.floor(seconds / 86400), '天'],
    [Math.floor((seconds % 86400) / 3600), '小时'],
    [Math.floor((seconds % 3600) / 60), '分钟'],
  ]
  const first = parts.findIndex(([n]) => n > 0)
  if (first < 0) return [[String(Math.floor(seconds)), '秒']]
  return parts
    .slice(first, first + 2)
    .filter(([n], i) => i === 0 || n > 0)
    .map(([n, unit]) => [String(n), unit])
}

const dateFormat = new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: 'numeric', day: 'numeric' })
const shortDate = new Intl.DateTimeFormat('zh-CN', { month: 'numeric', day: 'numeric' })
const dateTime = new Intl.DateTimeFormat('zh-CN', {
  year: 'numeric',
  month: 'numeric',
  day: 'numeric',
  hour: '2-digit',
  minute: '2-digit',
})

export const formatDateTime = (unix: number) => dateTime.format(unix * 1000)

/** 相对时间，超过一周显示日期 */
export function formatRelative(unix: number, now = Date.now()): string {
  const diff = Math.max(0, now / 1000 - unix)
  if (diff < 60) return '刚刚'
  if (diff < 3600) return `${Math.floor(diff / 60)}分钟前`
  if (diff < 86400) return `${Math.floor(diff / 3600)}小时前`
  if (diff < 7 * 86400) return `${Math.floor(diff / 86400)}天前`
  const date = new Date(unix * 1000)
  return date.getFullYear() === new Date(now).getFullYear() ? shortDate.format(date) : dateFormat.format(date)
}

/** YYYY-MM-DD -> M月D日 */
export function formatDay(day: string): string {
  const [, m, d] = day.split('-')
  return `${Number(m)}月${Number(d)}日`
}

/** YYYY-MM-DD -> M/D */
export function formatDayShort(day: string): string {
  const [, m, d] = day.split('-')
  return `${Number(m)}/${Number(d)}`
}
