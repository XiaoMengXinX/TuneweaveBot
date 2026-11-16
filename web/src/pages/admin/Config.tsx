import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useState } from 'react'
import { Icon } from '../../components/Icon'
import { toast } from '../../components/Toast'
import { CellSkeleton, ErrorState } from '../../components/ui'
import { get, post, type ConfigView } from '../../lib/api'
import { useAwaitReload } from '../../lib/hooks'
import { confirmDialog, useMainButton } from '../../lib/telegram'

export function Config() {
  const client = useQueryClient()
  const awaitReload = useAwaitReload()
  const config = useQuery({ queryKey: ['admin', 'config'], queryFn: () => get<ConfigView>('/admin/config') })
  const current =
    config.data?.sections.find((s) => s.name === 'bot')?.items.find((i) => i.key === 'cache_chat')?.value ?? '0'
  const [chat, setChat] = useState('')
  useEffect(() => setChat(current === '0' ? '' : current), [current])

  const setCacheChat = useMutation({
    mutationFn: (chatId: number) => post<{ warning: string }>('/admin/cache-chat', { chat_id: chatId }),
    onSuccess: (result, chatId) => {
      if (result.warning) toast.error(result.warning)
      else toast.ok(chatId === 0 ? '已关闭缓存群' : '缓存群已设置')
      client.invalidateQueries({ queryKey: ['admin', 'config'] })
      client.invalidateQueries({ queryKey: ['admin', 'status'] })
    },
    onError: (error) => toast.error(error),
  })
  const reload = useMutation({
    mutationFn: () => post('/admin/reload'),
    onSuccess: () => awaitReload(),
    onError: (error) => toast.error(error),
  })

  const input = chat.trim()
  const valid = /^-?\d+$/.test(input)
  const dirty = valid && String(Number(input)) !== current
  const { mutate } = setCacheChat
  const onSave = useCallback(() => mutate(Number(input)), [mutate, input])
  useMainButton(dirty ? { text: '保存缓存群', onClick: onSave, loading: setCacheChat.isPending } : null)

  return (
    <div className="page sub">
      <h1 className="large-title">配置</h1>
      {config.isPending ? (
        <CellSkeleton rows={6} side={false} />
      ) : config.isError ? (
        <ErrorState error={config.error} onRetry={config.refetch} />
      ) : (
        <>
          <p className="page-lead selectable">
            当前生效的配置，来自<span className="mono">{config.data.path}</span>及环境变量。凭据不会显示。
          </p>

          <h2 className="section-title" style={{ marginTop: 8 }}>
            缓存群
          </h2>
          <div className="card pad stack">
            <input
              className="text-input mono"
              inputMode="text"
              placeholder="群ID，如 -1001234567890"
              autoComplete="off"
              value={chat}
              onChange={(e) => setChat(e.target.value)}
            />
            {current !== '0' && (
              <button
                className="btn danger small"
                style={{ alignSelf: 'flex-start' }}
                disabled={setCacheChat.isPending}
                onClick={() => setCacheChat.mutate(0)}
              >
                关闭缓存群
              </button>
            )}
          </div>
          <p className="section-note">
            Guest Mode点歌时，未缓存的歌曲先上传到缓存群。保存时bot会向该群发一条测试消息，发送失败则不生效。
          </p>

          {config.data.sections.map((section) => (
            <section key={section.name}>
              <h2 className="section-title mono">[{section.name}]</h2>
              <dl className="card">
                {section.items.map((item) => (
                  <div className="kv" key={item.key}>
                    <dt className="mono">{item.key}</dt>
                    <dd className="mono selectable">
                      {item.secret ? (
                        <span className={`badge ${item.set ? 'ok' : ''}`}>{item.set ? '已设置' : '未设置'}</span>
                      ) : (
                        item.value || <span style={{ color: 'var(--text-3)' }}>空</span>
                      )}
                    </dd>
                  </div>
                ))}
              </dl>
            </section>
          ))}

          <h2 className="section-title">重载</h2>
          <button
            className="btn"
            disabled={reload.isPending}
            onClick={async () => {
              if (await confirmDialog('重新加载配置与插件？bot会先等当前任务完成，期间不接收新消息。')) reload.mutate()
            }}
          >
            {reload.isPending ? <span className="spinner" /> : <Icon name="refresh" size={18} />}
            重载配置与插件
          </button>
          <p className="section-note">修改配置文件后在这里重载，无需重启进程。</p>
        </>
      )}
    </div>
  )
}
