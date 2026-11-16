import { initData } from './telegram'
import { lang, t } from './i18n'

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message)
  }
}

type Handler = (method: string, path: string, body: unknown) => Promise<unknown>

// VITE_MOCK 为构建期常量：生产构建中这个分支与 mock 数据都会被剔除
const mock: Promise<Handler> | null = import.meta.env.VITE_MOCK
  ? import('../mock').then((m) => m.handle as Handler)
  : null

export async function api<T>(method: string, path: string, body?: unknown): Promise<T> {
  if (mock) return (await mock)(method, path, body) as Promise<T>
  let res: Response
  try {
    res = await fetch('api' + path, {
      method,
      headers: {
        Authorization: 'tma ' + initData,
        'X-Language': lang,
        ...(body !== undefined && { 'Content-Type': 'application/json' }),
      },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
  } catch {
    throw new ApiError(0, t.common.networkError)
  }
  const data = await res.json().catch(() => null)
  if (!res.ok) throw new ApiError(res.status, data?.error ?? t.common.requestFailed(res.status))
  // 代理或网关可能以 200 返回自己的页面
  if (data === null) throw new ApiError(res.status, t.common.badResponse)
  return data as T
}

export const get = <T>(path: string) => api<T>('GET', path)
export const post = <T = unknown>(path: string, body?: unknown) => api<T>('POST', path, body ?? {})
export const del = <T = unknown>(path: string) => api<T>('DELETE', path)

export function query(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') search.set(key, String(value))
  }
  const text = search.toString()
  return text ? '?' + text : ''
}

// ---- 数据类型，对应 internal/bot/webapp_*.go ----

export interface Provider {
  name: string
  display: string
  default: boolean
}

export interface Me {
  id: number
  name: string
  admin: boolean
  bot: string
  providers: Provider[]
}

export interface Overview {
  songs: number
  duration: number
  size: number
  total_cached: number
}

export interface Song {
  id: number
  provider: string
  provider_name: string
  track_id: string
  title: string
  artists: string
  album: string
  ext: string
  size: number
  bit_rate: number
  duration: number
  cached_at: number
}

/** 我的歌曲的排序方式，对应 store.SongOrder */
export type SongOrder = 'recent' | 'oldest' | 'az' | 'za'

export interface UserSong extends Song {
  last_at: number
  times: number
}

export interface Page<T> {
  items: T[]
  total: number
}

export interface SearchItem {
  provider: string
  id: string
  sub?: string
  provider_name: string
  title: string
  artists: string
  album: string
  duration: number
  cached: boolean
}

export interface SearchResults {
  items: SearchItem[]
}

export type AuthState = 'ok' | 'expired' | 'none'

export interface Status {
  version: string
  commit: string
  build_time: string
  runtime: string
  arch: string
  started_at: number
  providers: (Provider & { auth: AuthState })[]
  /** 字节：sys 是向系统申请的内存，heap 是使用中的堆 */
  memory: { sys: number; heap: number }
  cache_chat: { id: number; name: string }
  cache_total: number
  /** 缓存歌曲的文件总大小，字节 */
  cache_size: number
  db_size: number
  cache_dir_size: number
}

export type DownloadPhase = 'waiting' | 'resolving' | 'downloading' | 'processing' | 'uploading'

export interface DownloadTask {
  id: number
  provider: string
  track_id: string
  title: string
  artists: string
  album: string
  phase: DownloadPhase
  written: number
  /** 未知时为 0 */
  total: number
  /** 字节/秒 */
  speed: number
  user_id: number
  user_name: string
  chat_id: number
  chat_name: string
  source: string
  queued_at: number
  /** 尚未拿到下载位时为 0 */
  started_at: number
}

export interface Downloads {
  limit: number
  tasks: DownloadTask[]
}

export interface DayCount {
  day: string
  count: number
}

export interface Stats {
  cached: DayCount[]
  requests: DayCount[]
  top_songs: (Song & { count: number })[]
  top_users: { id: number; name: string; count: number }[]
  top_chats: { id: number; name: string; count: number }[]
  cache_by_provider: { key: string; name: string; count: number }[]
  requests_by_provider: { key: string; name: string; count: number }[]
  requests_by_source: { key: string; count: number }[]
}

export interface CacheDetail {
  song: Song
  page_url: string
  from_user_id: number
  from_user_name: string
  from_chat_id: number
  from_chat_name: string
  updated_at: number
  requests: number
}

export interface LoginSession {
  id: string
  provider: string
  type: 'qr' | 'link' | 'token'
  content: string
  qr: string
  expires_at: number
  state: 'waiting' | 'scanned' | 'success' | 'failed' | 'expired' | 'cancelled' | 'reloading' | 'check_failed'
  nickname: string
  note: string
}

export interface LoginState {
  providers: { name: string; display: string; available: boolean }[]
  session: LoginSession | null
  busy: boolean
}

export interface PluginInfo {
  name: string
  display_name: string
  version: string
  build: string
  capabilities: string[]
  instances: number
  default: boolean
  updatable: boolean
  path: string
  auth: AuthState
}

export interface Plugins {
  plugins: PluginInfo[]
  rollbacks: { name: string; display_name: string; version: string; error: string }[]
  updating: boolean
}

export interface PluginUpdate {
  name: string
  display_name: string
  from: string
  to: string
  error?: string
}

export interface UpdateCheck {
  updates: PluginUpdate[]
  checked: number
  error: string
}

export interface ConfigView {
  path: string
  sections: { name: string; items: { key: string; value: string; secret: boolean; set: boolean }[] }[]
}

export interface Logs {
  date: string
  today: string
  /** 可切换的日期，新的在前 */
  dates: string[]
  entries: LogEntry[]
}

export interface LogEntry {
  time: string
  level: string
  message: string
  caller: string
}
