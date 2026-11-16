import { useEffect, useSyncExternalStore } from 'react'

// Telegram Mini App SDK 的最小类型声明，只包含用到的部分
interface TgButton {
  setText(text: string): void
  show(): void
  hide(): void
  enable(): void
  disable(): void
  showProgress(leaveActive?: boolean): void
  hideProgress(): void
  onClick(fn: () => void): void
  offClick(fn: () => void): void
}

interface TgWebApp {
  initData: string
  colorScheme: 'light' | 'dark'
  platform: string
  ready(): void
  expand(): void
  isVersionAtLeast(version: string): boolean
  onEvent(event: string, fn: () => void): void
  offEvent(event: string, fn: () => void): void
  setHeaderColor(color: string): void
  setBackgroundColor(color: string): void
  setBottomBarColor?(color: string): void
  openLink(url: string): void
  showConfirm(message: string, callback: (ok: boolean) => void): void
  BackButton: { show(): void; hide(): void; onClick(fn: () => void): void; offClick(fn: () => void): void }
  MainButton: TgButton
  HapticFeedback: {
    impactOccurred(style: 'light' | 'medium' | 'heavy'): void
    notificationOccurred(type: 'success' | 'warning' | 'error'): void
    selectionChanged(): void
  }
}

const wa = (window as unknown as { Telegram?: { WebApp?: TgWebApp } }).Telegram?.WebApp

/** 是否运行在 Telegram 内；浏览器直接打开时 SDK 存在但 initData 为空 */
export const inTelegram = !!wa?.initData
export const initData = wa?.initData ?? ''

const supports = (version: string) => inTelegram && wa!.isVersionAtLeast(version)

export const haptic = {
  tap: () => supports('6.1') && wa!.HapticFeedback.impactOccurred('light'),
  select: () => supports('6.1') && wa!.HapticFeedback.selectionChanged(),
  success: () => supports('6.1') && wa!.HapticFeedback.notificationOccurred('success'),
  error: () => supports('6.1') && wa!.HapticFeedback.notificationOccurred('error'),
}

// ---- 明暗模式 ----

const forcedScheme = new URLSearchParams(location.search).get('theme')

function currentScheme(): 'light' | 'dark' {
  if (forcedScheme === 'dark' || forcedScheme === 'light') return forcedScheme
  if (inTelegram) return wa!.colorScheme
  return matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

// 页面背景色（tokens.css 的 --bg）的 sRGB 值：浅色为 slate-2，深色为 slate-1。
// Telegram 只接受 #RRGGBB，而广色域屏幕上 CSS 变量解析出的是 color(display-p3 …)，不能直接读取。
const CHROME_COLOR = { light: '#f9f9fb', dark: '#111113' }

function applyScheme() {
  const scheme = currentScheme()
  document.documentElement.classList.toggle('dark', scheme === 'dark')
  if (!inTelegram) return
  // Telegram 的标题栏与底栏跟随页面背景
  const color = CHROME_COLOR[scheme]
  if (supports('6.1')) {
    wa!.setHeaderColor(color)
    wa!.setBackgroundColor(color)
  }
  if (supports('7.10')) wa!.setBottomBarColor?.(color)
}

export function initTelegram() {
  applyScheme()
  if (inTelegram) {
    wa!.onEvent('themeChanged', applyScheme)
    wa!.ready()
    wa!.expand()
  } else {
    matchMedia('(prefers-color-scheme: dark)').addEventListener('change', applyScheme)
  }
}

export function openLink(url: string) {
  // 链接来自插件或缓存数据，只打开网页地址
  if (!/^https?:\/\//i.test(url)) return
  if (inTelegram) wa!.openLink(url)
  else window.open(url, '_blank', 'noopener')
}

export function confirmDialog(message: string): Promise<boolean> {
  if (supports('6.2')) return new Promise((resolve) => wa!.showConfirm(message, resolve))
  return Promise.resolve(window.confirm(message))
}

// ---- BackButton 与 MainButton ----
// Telegram 外（本地预览）没有原生按钮，状态存入 store，由 <NativeFallback> 渲染成页面内按钮

export interface MainButtonState {
  text: string
  onClick: () => void
  loading?: boolean
  disabled?: boolean
}

interface NativeState {
  back: (() => void) | null
  main: MainButtonState | null
}

let native: NativeState = { back: null, main: null }
const listeners = new Set<() => void>()

function setNative(patch: Partial<NativeState>) {
  native = { ...native, ...patch }
  listeners.forEach((fn) => fn())
}

export function useNativeFallback(): NativeState {
  return useSyncExternalStore(
    (fn) => {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
    () => native,
  )
}

/** onBack 非空时显示返回按钮 */
export function useBackButton(onBack: (() => void) | null) {
  useEffect(() => {
    if (!onBack) return
    if (supports('6.1')) {
      wa!.BackButton.onClick(onBack)
      wa!.BackButton.show()
      return () => {
        wa!.BackButton.offClick(onBack)
        wa!.BackButton.hide()
      }
    }
    setNative({ back: onBack })
    return () => setNative({ back: null })
  }, [onBack])
}

/** state 非空时显示主按钮；onClick 须保持引用稳定 */
export function useMainButton(state: MainButtonState | null) {
  const { text, onClick, loading = false, disabled = false } = state ?? { text: '', onClick: undefined }
  useEffect(() => {
    if (!onClick) return
    if (!inTelegram) {
      setNative({ main: { text, onClick, loading, disabled } })
      return () => setNative({ main: null })
    }
    const button = wa!.MainButton
    button.setText(text)
    if (disabled || loading) button.disable()
    else button.enable()
    if (loading) button.showProgress(true)
    else button.hideProgress()
    button.onClick(onClick)
    button.show()
    return () => {
      button.offClick(onClick)
      button.hideProgress()
      button.hide()
    }
  }, [text, onClick, loading, disabled])
}
