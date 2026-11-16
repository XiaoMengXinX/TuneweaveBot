import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useEffect, useState, useSyncExternalStore } from 'react'
import { toast } from '../components/Toast'
import { get, post } from './api'
import { haptic } from './telegram'
import { t } from './i18n'

export function useDebounced<T>(value: T, ms = 250): T {
  const [debounced, setDebounced] = useState(value)
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), ms)
    return () => clearTimeout(timer)
  }, [value, ms])
  return debounced
}

export interface SongRef {
  provider: string
  id: string
  sub?: string
}

export const refKey = (ref: SongRef) => `${ref.provider}:${ref.sub ?? ''}:${ref.id}`

/** 让 bot 把歌曲发到私聊；sending 是正在提交的歌曲 key */
export function useSendSong() {
  const client = useQueryClient()
  const mutation = useMutation({
    mutationFn: (ref: SongRef) => post('/music', ref),
    onSuccess: () => {
      toast.ok(t.send.done)
      // 点歌记录稍后才写入，延迟刷新概览与列表
      setTimeout(() => {
        client.invalidateQueries({ queryKey: ['overview'] })
        client.invalidateQueries({ queryKey: ['songs'] })
      }, 4000)
    },
    onError: (error) => toast.error(error),
  })
  return {
    send(ref: SongRef) {
      if (mutation.isPending) return
      haptic.tap()
      mutation.mutate(ref)
    },
    sending: mutation.isPending && mutation.variables ? refKey(mutation.variables) : null,
  }
}

// ---- 重载等待 ----
// bot 重载期间 HTTP 服务会短暂停止，这里轮询到服务恢复后再刷新全部数据

let reloading = false
const listeners = new Set<() => void>()
const setReloading = (value: boolean) => {
  reloading = value
  listeners.forEach((fn) => fn())
}

export function useReloading(): boolean {
  return useSyncExternalStore(
    (fn) => {
      listeners.add(fn)
      return () => listeners.delete(fn)
    },
    () => reloading,
  )
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms))

export function useAwaitReload() {
  const client = useQueryClient()
  return async () => {
    if (reloading) return
    setReloading(true)
    // 在途任务结束后才真正重载，先等服务停下，再等它恢复
    const deadline = Date.now() + 10 * 60 * 1000
    let wentDown = false
    await sleep(1200)
    while (Date.now() < deadline) {
      try {
        await get('/me')
        if (wentDown) break
      } catch {
        wentDown = true
      }
      await sleep(1200)
    }
    setReloading(false)
    if (Date.now() >= deadline) {
      toast.error(t.common.reloadTimeout)
      return
    }
    await client.invalidateQueries()
    toast.ok(t.common.reloadDone)
  }
}
