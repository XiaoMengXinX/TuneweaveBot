// 本地预览用的假数据（npm run dev:mock），生产构建不包含本模块。
import { ApiError } from '../lib/api'
import type * as T from '../lib/api'

const params = new URLSearchParams(location.search)
const isAdmin = params.get('role') !== 'user'
const emptyUser = params.get('empty') === '1'

// 固定种子的伪随机数，保证每次预览的数据一致
let seed = 20261116
const rand = () => {
  seed = (seed * 1664525 + 1013904223) >>> 0
  return seed / 2 ** 32
}
const pick = <V>(list: V[]) => list[Math.floor(rand() * list.length)]

const TITLES = [
  '晴天', '夜に駆ける', '七里香', 'Lemon', '稻香', '群青', '起风了', 'Shape of You', '海阔天空', '怪物',
  '后来', 'Blinding Lights', '富士山下', '打上花火', '演员', 'Viva La Vida', '光年之外', '春日影', '平凡之路',
  'Counting Stars', '倒数', '花に亡霊', '匆匆那年', 'Yellow', '可惜没如果', 'アイドル', '小幸运', 'Bad Guy',
  '红豆', 'ただ君に晴れ', '若把你', 'Someone Like You', '消愁', '炎', '体面', 'Levitating', '如愿',
  'A Very Long Song Title That Should Be Truncated Gracefully (Live at Somewhere 2024 Remastered)',
]
const ARTISTS = ['周杰伦', 'YOASOBI', '米津玄師', '陈奕迅', 'Ed Sheeran', '邓紫棋', 'ヨルシカ', 'Coldplay', '毛不易', '王菲', 'Adele', '朴树']
const ALBUMS = ['叶惠美', 'THE BOOK', 'STRAY SHEEP', '÷ (Deluxe)', '摩天动物园', '盗作', 'Parachutes', '平凡的一天', '']
const PROVIDERS: T.Provider[] = [
  { name: 'netease', display: '网易云音乐', default: true },
  { name: 'qqmusic', display: 'QQ音乐', default: false },
]

const now = Math.floor(Date.now() / 1000)
const songs: T.Song[] = Array.from({ length: 1280 }, (_, i) => {
  const provider = rand() < 0.72 ? PROVIDERS[0] : PROVIDERS[1]
  const flac = rand() < 0.6
  const duration = 150 + Math.floor(rand() * 180)
  return {
    id: 1280 - i,
    provider: provider.name,
    provider_name: provider.display,
    track_id: String(100000 + Math.floor(rand() * 9e8)),
    title: pick(TITLES),
    artists: rand() < 0.2 ? `${pick(ARTISTS)}/${pick(ARTISTS)}` : pick(ARTISTS),
    album: pick(ALBUMS),
    ext: flac ? 'flac' : 'mp3',
    size: Math.floor(duration * (flac ? 118000 : 40000)),
    bit_rate: flac ? 944000 : 320000,
    duration,
    cached_at: now - i * 5400 - Math.floor(rand() * 3000),
  }
})

const mine: T.UserSong[] = emptyUser
  ? []
  : songs
      .filter((_, i) => i % 3 === 0)
      .map((song, i) => ({ ...song, last_at: now - 240 - i * 9100, times: 1 + Math.floor(rand() * 4) }))

const days = (n: number, base: number, wobble: number): T.DayCount[] =>
  Array.from({ length: n }, (_, i) => {
    const date = new Date(Date.now() - (n - 1 - i) * 86400_000)
    const day = `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
    const weekend = date.getDay() === 0 || date.getDay() === 6 ? 1.35 : 1
    return { day, count: Math.max(0, Math.round((base + (rand() - 0.5) * wobble) * weekend * (0.75 + (i / n) * 0.5))) }
  })

let cacheChat = -1001234567890
let login: T.LoginSession | null = null
let loginTimer = 0
let authOK = params.get('expired') !== '1'

const match = (song: T.Song, q: string) =>
  !q || [song.title, song.artists, song.album].some((v) => v.toLowerCase().includes(q.toLowerCase())) || song.track_id === q

function page<V>(list: V[], search: URLSearchParams): T.Page<V> {
  const offset = Number(search.get('offset') ?? 0)
  const limit = Number(search.get('limit') ?? 100)
  return { items: list.slice(offset, offset + limit), total: list.length }
}

const QR =
  'data:image/svg+xml;utf8,' +
  encodeURIComponent(
    `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 29 29" shape-rendering="crispEdges"><rect width="29" height="29" fill="#fff"/>${Array.from(
      { length: 29 * 29 },
      (_, i) => {
        const [x, y] = [i % 29, Math.floor(i / 29)]
        const finder = (cx: number, cy: number) => {
          const [dx, dy] = [Math.abs(x - cx), Math.abs(y - cy)]
          const d = Math.max(dx, dy)
          return d <= 3 ? d !== 2 : null
        }
        const f = finder(3, 3) ?? finder(25, 3) ?? finder(3, 25)
        const on = f ?? ((x * 7 + y * 13 + ((x * y) % 5)) % 3 === 0)
        return on ? `<rect x="${x}" y="${y}" width="1" height="1"/>` : ''
      },
    ).join('')}</svg>`,
  )

const LOG_MESSAGES: [string, string, string][] = [
  ['INFO', 'netease 登录成功: 小明', 'cookie_watch.go:109'],
  ['INFO', '看板服务已启动: 127.0.0.1:8080 -> https://bot.example.com/', 'webapp.go:52'],
  ['WARNING', 'telegram: Too Many Requests: retry after 3', 'handlers_music.go:38'],
  ['ERROR', '下载失败: Get "https://m801.music.126.net/…/a.flac": context deadline exceeded', 'handlers_music.go:128'],
  ['INFO', '歌曲无可用音源: netease 1901371647, no audio url: 版权方要求，当前歌曲暂无法播放 (code 404, fee 1)', 'handlers_music.go:112'],
  ['DEBUG', 'SQL [0.212ms] [rows:1] SELECT * FROM `song_infos` WHERE provider = "netease" AND track_id = "186016" AND `song_infos`.`deleted_at` IS NULL ORDER BY `song_infos`.`id` LIMIT 1', 'store.go:103'],
  ['ERROR', 'panic: runtime error: index out of range [3] with length 3\ngoroutine 88 [running]:\nruntime/debug.Stack()\n\t/usr/local/go/src/runtime/debug/stack.go:26 +0x5e\ngithub.com/XiaoMengXinX/TuneweaveBot/internal/bot.(*App).recoverMiddleware.func1.1()', 'middleware.go:19'],
  ['INFO', '插件 netease 已更新到 v1.4.2（2026-11-10 12:00:00）', 'handlers_update.go:226'],
]

const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))

export async function handle(method: string, url: string, body: unknown): Promise<unknown> {
  await wait(Number(params.get('delay') ?? 280))
  const [path, qs = ''] = url.split('?')
  const search = new URLSearchParams(qs)
  const route = `${method} ${path}`
  const data = body as Record<string, never>

  if (route.startsWith('GET /admin') && !isAdmin) throw new ApiError(403, '需要管理员权限')

  switch (route) {
    case 'GET /me':
      return { id: 42, name: '小孟', admin: isAdmin, bot: 'TuneweaveBot', providers: PROVIDERS } satisfies T.Me
    case 'GET /overview':
      return {
        songs: mine.length,
        duration: mine.reduce((n, s) => n + s.duration, 0),
        size: mine.reduce((n, s) => n + s.size, 0),
        total_cached: songs.length,
      } satisfies T.Overview
    case 'GET /songs': {
      const list = mine.filter((s) => match(s, search.get('q') ?? ''))
      if (search.get('order') === 'asc') list.reverse()
      return page(list, search)
    }
    case 'GET /search': {
      const q = search.get('q') ?? ''
      if (q === 'nothing') return { items: [], total: 0, page_size: 8 }
      const provider = PROVIDERS.find((p) => p.name === search.get('provider')) ?? PROVIDERS[0]
      const offset = Number(search.get('offset') ?? 0)
      const items: T.SearchItem[] = Array.from({ length: offset >= 24 ? 0 : 8 }, (_, i) => ({
        provider: provider.name,
        id: String(5000 + offset + i),
        provider_name: provider.display,
        title: i === 0 && offset === 0 ? q : `${q} ${TITLES[(offset + i * 3) % TITLES.length]}`,
        artists: ARTISTS[(offset + i) % ARTISTS.length],
        album: ALBUMS[(offset + i) % ALBUMS.length],
        duration: 180 + ((offset + i) * 17) % 120,
        cached: (offset + i) % 3 === 0,
      }))
      return { items, total: 24, page_size: 8 } satisfies T.SearchPage
    }
    case 'POST /music':
      if (params.get('cooldown') === '1') throw new ApiError(429, '操作太频繁，请 8 秒后再试')
      return {}

    case 'GET /admin/status':
      return {
        version: 'v1.6.0',
        commit: 'def95a7c0ffee1234567890abcdef1234567890ab',
        build_time: '2026-11-16 10:24:08',
        runtime: 'go1.26.0',
        arch: 'linux/amd64',
        started_at: now - 3 * 86400 - 4 * 3600 - 1200,
        providers: [
          { ...PROVIDERS[0], auth: authOK ? 'ok' : 'expired' },
          { ...PROVIDERS[1], auth: 'none' },
        ],
        downloads: { active: 3, waiting: 2, limit: 4 },
        cache_chat: { id: cacheChat, name: cacheChat ? 'Tuneweave Cache' : '' },
        cache_total: songs.length,
        db_size: 18.4 * 1024 * 1024,
        cache_dir_size: 312 * 1024 * 1024,
      } satisfies T.Status
    case 'GET /admin/stats': {
      const n = Number(search.get('days'))
      const scale = search.get('provider') ? 0.6 : 1
      const top = songs.slice(0, 10).map((song, i) => ({ ...song, count: Math.round((96 - i * 8.5) * scale * (n / 7)) }))
      return {
        requests: days(n, 46 * scale, 30),
        cached: days(n, 14 * scale, 12),
        top_songs: top,
        top_users: ['小孟', 'Alice', '', '音乐爱好者', 'Bob', '', 'たろう', 'Carol'].map((name, i) => ({
          id: 100000000 + i * 7919,
          name,
          count: Math.round((140 - i * 16) * scale * (n / 7)),
        })),
        top_chats: ['音乐分享群', '小孟', '深夜电台', '', 'Lossless Club'].map((name, i) => ({
          id: i === 1 ? 42 : -1001000000000 - i * 104729,
          name,
          count: Math.round((210 - i * 38) * scale * (n / 7)),
        })),
        cache_by_provider: [
          { key: 'netease', name: '网易云音乐', count: 921 },
          { key: 'qqmusic', name: 'QQ音乐', count: 359 },
        ],
        requests_by_provider: [
          { key: 'netease', name: '网易云音乐', count: 236 * (n / 7) },
          { key: 'qqmusic', name: 'QQ音乐', count: 88 * (n / 7) },
        ],
        requests_by_source: [
          { key: 'link', count: Math.round(142 * scale * (n / 7)) },
          { key: 'command', count: Math.round(96 * scale * (n / 7)) },
          { key: 'callback', count: Math.round(51 * scale * (n / 7)) },
          { key: 'webapp', count: Math.round(22 * scale * (n / 7)) },
          { key: 'guest', count: Math.round(9 * scale * (n / 7)) },
          { key: 'start', count: Math.round(4 * scale * (n / 7)) },
        ],
      } satisfies T.Stats
    }
    case 'GET /admin/cache':
      return page(
        songs.filter((s) => match(s, search.get('q') ?? '') && (!search.get('provider') || s.provider === search.get('provider'))),
        search,
      )
    case 'GET /admin/login':
      return { providers: [{ name: 'netease', display: '网易云音乐', available: authOK }], session: login, busy: false } satisfies T.LoginState
    case 'POST /admin/login': {
      const token = data.mode === 'token'
      login = {
        id: String(Date.now()),
        provider: 'netease',
        type: token ? 'token' : 'qr',
        content: token ? '请回复 MUSIC_U cookie 的值，可在浏览器开发者工具中找到。' : '',
        qr: token ? '' : QR,
        expires_at: now + 180,
        state: 'waiting',
        nickname: '',
        note: '',
      }
      login.expires_at = Math.floor(Date.now() / 1000) + 180
      if (!token) {
        // 演示扫码流程：先变为已扫码，再登录成功
        const session = login
        window.clearTimeout(loginTimer)
        loginTimer = window.setTimeout(() => {
          if (login !== session || session.state !== 'waiting') return
          session.state = 'scanned'
          session.nickname = '小明'
          loginTimer = window.setTimeout(() => {
            if (login !== session) return
            session.state = 'success'
            authOK = true
          }, 4000)
        }, 6000)
      }
      return { providers: [{ name: 'netease', display: '网易云音乐', available: authOK }], session: login, busy: false }
    }
    case 'POST /admin/login/token':
      if (login) {
        login.state = String(data.token).length > 5 ? 'success' : 'failed'
        login.nickname = '小明'
        authOK = authOK || login.state === 'success'
      }
      return {}
    case 'DELETE /admin/login':
      if (login) login.state = 'cancelled'
      return {}
    case 'GET /admin/plugins':
      return {
        plugins: [
          { name: 'netease', display_name: '网易云音乐', version: 'v1.4.1', build: 'a1b2c3d 2026-10-28 09:12:44', capabilities: ['provider', 'auth', 'recognize'], instances: 2, default: true, updatable: true, path: 'plugins/netease.wasm.gz', auth: authOK ? 'ok' : 'expired' },
          { name: 'qqmusic', display_name: 'QQ音乐', version: 'v0.3.0', build: '', capabilities: ['provider'], instances: 2, default: false, updatable: false, path: 'plugins/qqmusic.wasm', auth: 'none' },
        ],
        rollbacks: params.get('rollback') === '1' ? [{ name: 'netease', display_name: '网易云音乐', version: 'v1.4.1（2026-10-28 09:12:44）', error: 'manifest: abi version 3 not supported' }] : [],
        updating: false,
      } satisfies T.Plugins
    case 'POST /admin/plugins/check':
      await wait(700)
      return { updates: [{ name: 'netease', display_name: '网易云音乐', from: 'v1.4.1', to: 'v1.4.2' }], checked: 1, error: '' } satisfies T.UpdateCheck
    case 'POST /admin/plugins/update':
      await wait(1200)
      return { results: [{ name: 'netease', display_name: '网易云音乐', from: 'v1.4.1', to: 'v1.4.2' }], reloading: true }
    case 'GET /admin/config':
      return {
        path: 'config.toml',
        sections: [
          { name: 'bot', items: [
            { key: 'token', value: '', secret: true, set: true },
            { key: 'api_url', value: 'https://api.telegram.org', secret: false, set: false },
            { key: 'admin', value: '[42, 1919810]', secret: false, set: false },
            { key: 'debug', value: 'false', secret: false, set: false },
            { key: 'local_mode', value: 'false', secret: false, set: false },
            { key: 'upload_timeout', value: '2m0s', secret: false, set: false },
            { key: 'cache_chat', value: String(cacheChat), secret: false, set: false },
          ] },
          { name: 'webapp', items: [
            { key: 'listen', value: '127.0.0.1:8080', secret: false, set: false },
            { key: 'url', value: 'https://bot.example.com/', secret: false, set: false },
          ] },
          { name: 'plugins', items: [
            { key: 'dir', value: './plugins', secret: false, set: false },
            { key: 'enabled', value: '[]', secret: false, set: false },
            { key: 'default', value: 'netease', secret: false, set: false },
            { key: 'instances', value: '2', secret: false, set: false },
            { key: 'call_timeout', value: '30s', secret: false, set: false },
            { key: 'proxy', value: 'http://***@127.0.0.1:7890', secret: false, set: false },
            { key: 'update_cron', value: '0 4 * * *', secret: false, set: false },
          ] },
          { name: 'plugins.netease', items: [
            { key: 'level', value: 'hires', secret: false, set: false },
            { key: 'music_u', value: '', secret: true, set: true },
          ] },
          { name: 'download', items: [
            { key: 'timeout', value: '2m0s', secret: false, set: false },
            { key: 'retries', value: '3', secret: false, set: false },
            { key: 'concurrency', value: '4', secret: false, set: false },
          ] },
        ],
      } satisfies T.ConfigView
    case 'POST /admin/cache-chat':
      cacheChat = Number(data.chat_id)
      return { chat_id: cacheChat, warning: '' }
    case 'POST /admin/reload':
      return {}
    case 'GET /admin/logs': {
      const level = (search.get('level') ?? '').toUpperCase()
      const entries: T.LogEntry[] = Array.from({ length: 60 }, (_, i) => {
        const [lvl, message, caller] = LOG_MESSAGES[(i * 5 + 3) % LOG_MESSAGES.length]
        const at = new Date(Date.now() - i * 137_000)
        const pad = (v: number) => String(v).padStart(2, '0')
        return {
          time: `${at.getFullYear()}/${pad(at.getMonth() + 1)}/${pad(at.getDate())} ${pad(at.getHours())}:${pad(at.getMinutes())}:${pad(at.getSeconds())}`,
          level: lvl,
          message,
          caller,
        }
      })
      return { entries: entries.filter((e) => !level || e.level === level) }
    }
  }

  const cache = route.match(/^(GET|DELETE) \/admin\/cache\/(\d+)$/)
  if (cache) {
    const song = songs.find((s) => s.id === Number(cache[2]))
    if (!song) throw new ApiError(404, '歌曲未缓存')
    if (cache[1] === 'DELETE') {
      songs.splice(songs.indexOf(song), 1)
      return {}
    }
    return {
      song,
      page_url: `https://music.163.com/song?id=${song.track_id}`,
      from_user_id: 100007919,
      from_user_name: 'alice',
      from_chat_id: -1001000104729,
      from_chat_name: '音乐分享群',
      updated_at: song.cached_at + 86400,
      requests: 17,
    } satisfies T.CacheDetail
  }
  const manifest = route.match(/^GET \/admin\/plugins\/([^/]+)\/manifest$/)
  if (manifest) {
    return {
      manifest: {
        name: manifest[1],
        display_name: manifest[1] === 'netease' ? '网易云音乐' : 'QQ音乐',
        version: 'v1.4.1',
        abi_version: 2,
        capabilities: ['provider', 'auth', 'recognize'],
        code: 'ne',
        link_hosts: ['music.163.com', '163cn.tv'],
        commands: [{ name: 'netease', description: '网易云点歌' }],
        update_url: 'https://github.com/XiaoMengXinX/TuneweaveBot/releases/download/plugins/netease.wasm.gz',
      },
    }
  }
  throw new ApiError(404, '接口不存在')
}
