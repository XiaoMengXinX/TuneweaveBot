import { useQuery } from '@tanstack/react-query'
import { useEffect, useRef } from 'react'
import { Icon, type IconName } from './components/Icon'
import { Toaster } from './components/Toast'
import { CellSkeleton, Empty, Skeleton } from './components/ui'
import { get, type Me } from './lib/api'
import { useReloading } from './lib/hooks'
import { back, switchTab, tabOf, useRoute, type Tab } from './lib/router'
import { haptic, useBackButton, useNativeFallback } from './lib/telegram'
import { Cache, CacheDetail } from './pages/admin/Cache'
import { Config } from './pages/admin/Config'
import { Hub } from './pages/admin/Hub'
import { Login } from './pages/admin/Login'
import { Logs } from './pages/admin/Logs'
import { PluginDetail, Plugins } from './pages/admin/Plugins'
import { Stats } from './pages/admin/Stats'
import { Overview } from './pages/Overview'
import { Search } from './pages/Search'
import { Songs } from './pages/Songs'

const TAB_ITEMS: { tab: Tab; label: string; icon: IconName; admin?: boolean }[] = [
  { tab: '/', label: '概览', icon: 'home' },
  { tab: '/songs', label: '歌曲', icon: 'music' },
  { tab: '/search', label: '搜索', icon: 'search' },
  { tab: '/admin', label: '管理', icon: 'sliders', admin: true },
]

function Shell() {
  return (
    <div className="app">
      <div className="views">
        <div className="page" aria-busy>
          <div className="greeting">
            <Skeleton width={40} height={14} />
            <div style={{ height: 10 }} />
            <Skeleton width={160} height={28} />
          </div>
          <CellSkeleton rows={5} />
        </div>
      </div>
    </div>
  )
}

/** 管理页的子页面；缓存列表在查看详情时保持挂载，返回后滚动位置不变 */
function AdminSubPage({ path, me }: { path: string; me: Me }) {
  const [, , section, param] = path.split('/')
  switch (section) {
    case 'stats':
      return <Stats me={me} />
    case 'cache':
      return (
        <>
          <Cache me={me} hidden={!!param} />
          {param && <CacheDetail key={param} id={param} />}
        </>
      )
    case 'login':
      return <Login />
    case 'plugins':
      return param ? <PluginDetail name={decodeURIComponent(param)} /> : <Plugins />
    case 'config':
      return <Config />
    case 'logs':
      return <Logs />
  }
  return null
}

export function App() {
  const me = useQuery({ queryKey: ['me'], queryFn: () => get<Me>('/me'), staleTime: Infinity })
  const route = useRoute()
  const reloading = useReloading()
  const fallback = useNativeFallback()

  const admin = me.data?.admin ?? false
  // 非管理员访问管理页地址时回到概览
  const path = route.startsWith('/admin') && me.data && !admin ? '/' : route
  const tab = tabOf(path)
  const isSub = path.startsWith('/admin/')
  useBackButton(isSub ? back : null)

  // tab 页首次访问后保持挂载，切换时保留滚动位置与输入
  const visited = useRef(new Set<Tab>())
  visited.current.add(tab)

  useEffect(() => {
    if (me.data) document.title = `@${me.data.bot}`
  }, [me.data])

  if (me.isPending) return <Shell />
  if (me.isError) {
    return (
      <div className="app">
        <Empty icon="alert" title="无法打开看板">
          {me.error.message}
        </Empty>
      </div>
    )
  }

  const has = (t: Tab) => visited.current.has(t)
  return (
    <div className="app">
      <main className="views">
        {has('/') && <Overview me={me.data} hidden={tab !== '/'} />}
        {has('/songs') && <Songs hidden={tab !== '/songs'} />}
        {has('/search') && <Search me={me.data} hidden={tab !== '/search'} />}
        {admin && has('/admin') && <Hub hidden={tab !== '/admin' || isSub} />}
        {admin && isSub && <AdminSubPage path={path} me={me.data} />}
      </main>

      {fallback.main && (
        <div className="fallback-main">
          <button className="btn primary" disabled={fallback.main.disabled || fallback.main.loading} onClick={fallback.main.onClick}>
            {fallback.main.loading && <span className="spinner" />}
            {fallback.main.text}
          </button>
        </div>
      )}
      {fallback.back && (
        <button className="fallback-back" onClick={fallback.back}>
          返回
        </button>
      )}

      {!isSub && (
        <nav className="tabbar">
          {TAB_ITEMS.filter((item) => !item.admin || admin).map((item) => (
            <button
              key={item.tab}
              aria-current={item.tab === tab ? 'page' : undefined}
              onClick={() => {
                if (item.tab === tab) return
                haptic.select()
                switchTab(item.tab)
              }}
            >
              <Icon name={item.icon} />
              {item.label}
            </button>
          ))}
        </nav>
      )}

      <Toaster />
      {reloading && (
        <div className="overlay" role="status">
          <span className="spinner" />
          正在重载，等待当前任务完成
        </div>
      )}
    </div>
  )
}
