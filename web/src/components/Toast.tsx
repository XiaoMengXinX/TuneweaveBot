import { useSyncExternalStore } from 'react'
import { haptic } from '../lib/telegram'
import { Icon } from './Icon'

interface ToastState {
  id: number
  text: string
  kind: 'ok' | 'error'
}

let current: ToastState | null = null
let timer = 0
const listeners = new Set<() => void>()

function show(text: string, kind: ToastState['kind']) {
  window.clearTimeout(timer)
  current = { id: Date.now(), text, kind }
  listeners.forEach((fn) => fn())
  timer = window.setTimeout(() => {
    current = null
    listeners.forEach((fn) => fn())
  }, 2600)
}

/** 全局轻提示，同时给出触感反馈 */
export const toast = {
  ok(text: string) {
    haptic.success()
    show(text, 'ok')
  },
  error(error: unknown) {
    haptic.error()
    show(error instanceof Error ? error.message : String(error), 'error')
  },
}

export function Toaster() {
  const state = useSyncExternalStore(
    (fn) => {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
    () => current,
  )
  if (!state) return null
  return (
    <div key={state.id} className={`toast ${state.kind}`} role="status">
      <Icon name={state.kind === 'ok' ? 'check' : 'alert'} size={18} />
      <span>{state.text}</span>
    </div>
  )
}
