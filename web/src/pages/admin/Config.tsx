import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useState } from 'react'
import { Icon } from '../../components/Icon'
import { toast } from '../../components/Toast'
import { CellSkeleton, ErrorState } from '../../components/ui'
import { get, post, type ConfigView } from '../../lib/api'
import { useAwaitReload } from '../../lib/hooks'
import { confirmDialog, useMainButton } from '../../lib/telegram'
import { t } from '../../lib/i18n'

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
      else toast.ok(chatId === 0 ? t.config.chatDisabled : t.config.chatSaved)
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
  useMainButton(dirty ? { text: t.config.saveChat, onClick: onSave, loading: setCacheChat.isPending } : null)

  return (
    <div className="page sub">
      <h1 className="large-title">{t.config.title}</h1>
      {config.isPending ? (
        <CellSkeleton rows={6} side={false} />
      ) : config.isError ? (
        <ErrorState error={config.error} onRetry={config.refetch} />
      ) : (
        <>
          <p className="page-lead selectable">
            {t.config.lead[0]}
            <span className="mono">{config.data.path}</span>
            {t.config.lead[1]}
          </p>

          <h2 className="section-title" style={{ marginTop: 8 }}>
            {t.config.cacheChat}
          </h2>
          <div className="card pad stack">
            <input
              className="text-input mono"
              inputMode="text"
              placeholder={t.config.chatPlaceholder}
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
                {t.config.disableChat}
              </button>
            )}
          </div>
          <p className="section-note">
            {t.config.chatNote}
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
                        <span className={`badge ${item.set ? 'ok' : ''}`}>{item.set ? t.config.secretSet : t.common.notSet}</span>
                      ) : (
                        item.value || <span style={{ color: 'var(--text-3)' }}>{t.common.empty}</span>
                      )}
                    </dd>
                  </div>
                ))}
              </dl>
            </section>
          ))}

          <h2 className="section-title">{t.config.reload}</h2>
          <button
            className="btn"
            disabled={reload.isPending}
            onClick={async () => {
              if (await confirmDialog(t.config.confirmReload)) reload.mutate()
            }}
          >
            {reload.isPending ? <span className="spinner" /> : <Icon name="refresh" size={18} />}
            {t.config.reloadButton}
          </button>
          <p className="section-note">{t.config.reloadNote}</p>
        </>
      )}
    </div>
  )
}
