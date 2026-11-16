import { useSyncExternalStore } from 'react'

// 基于 hash 的最小路由：tab 之间替换历史记录，子页面压栈，
// 这样 Telegram 的 BackButton 只需要对应"返回上一级"。

export const TABS = ['/', '/songs', '/search', '/admin'] as const
export type Tab = (typeof TABS)[number]

// Telegram 把启动参数放在 hash 里（#tgWebAppData=...），只有以 #/ 开头的 hash 才是页面路径
const read = () => (location.hash.startsWith('#/') ? location.hash.slice(1) : '/')

function subscribe(fn: () => void) {
  window.addEventListener('hashchange', fn)
  window.addEventListener('popstate', fn)
  return () => {
    window.removeEventListener('hashchange', fn)
    window.removeEventListener('popstate', fn)
  }
}

export function useRoute(): string {
  return useSyncExternalStore(subscribe, read)
}

const notify = () => window.dispatchEvent(new PopStateEvent('popstate'))

/** 进入子页面 */
export function push(path: string) {
  history.pushState({ depth: (history.state?.depth ?? 0) + 1 }, '', '#' + path)
  notify()
}

/** 切换 tab，不产生历史记录 */
export function switchTab(tab: Tab) {
  history.replaceState({ depth: 0 }, '', '#' + tab)
  notify()
}

/** 返回上一级；直接以子页面地址打开时没有可回退的记录，改为替换到上级路径 */
export function back() {
  if ((history.state?.depth ?? 0) > 0) {
    history.back()
    return
  }
  const parent = read().replace(/\/[^/]*$/, '') || '/'
  history.replaceState({ depth: 0 }, '', '#' + parent)
  notify()
}

export function tabOf(path: string): Tab {
  if (path.startsWith('/admin')) return '/admin'
  return (TABS as readonly string[]).includes(path) ? (path as Tab) : '/'
}
