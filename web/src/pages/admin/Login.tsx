import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Icon } from '../../components/Icon'
import { toast } from '../../components/Toast'
import { CellSkeleton, ErrorState } from '../../components/ui'
import { del, get, post, type LoginSession, type LoginState } from '../../lib/api'
import { haptic, openLink, useMainButton } from '../../lib/telegram'
import { t } from '../../lib/i18n'


const active = (s: LoginSession | null | undefined) => !!s && (s.state === 'waiting' || s.state === 'scanned')

/** 剩余秒数，每秒更新 */
function useCountdown(expiresAt: number): number {
  const [now, setNow] = useState(Date.now())
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [])
  return Math.max(0, Math.ceil(expiresAt - now / 1000))
}

export function Login() {
  const client = useQueryClient()
  const login = useQuery({
    queryKey: ['admin', 'login'],
    queryFn: () => get<LoginState>('/admin/login'),
    refetchInterval: (q) => (active(q.state.data?.session) ? 1500 : false),
  })
  const setState = (state: LoginState) => client.setQueryData(['admin', 'login'], state)
  // 只展示本次打开页面期间进行过的会话；服务端会保留上一次会话的终态
  const [watching, setWatching] = useState<string | null>(null)

  const begin = useMutation({
    mutationFn: (vars: { provider: string; mode: string }) => post<LoginState>('/admin/login', vars),
    onSuccess: setState,
    onError: (error) => toast.error(error),
  })

  // 会话进入终态时给出触感反馈并刷新登录状态
  const session = login.data?.session ?? null
  const lastState = useRef<string | null>(null)
  useEffect(() => {
    if (!session) return
    if (active(session)) setWatching(session.id)
    const key = session.id + session.state
    if (lastState.current && lastState.current !== key && !active(session)) {
      if (session.state === 'success') haptic.success()
      else haptic.error()
      client.invalidateQueries({ queryKey: ['admin', 'status'] })
    }
    lastState.current = key
  }, [session, client])

  const data = login.data
  const shown = session && (active(session) || session.id === watching) ? session : null

  return (
    <div className="page sub">
      <h1 className="large-title">{t.login.title}</h1>
      {login.isPending ? (
        <CellSkeleton rows={2} />
      ) : login.isError && !data ? (
        <ErrorState error={login.error} onRetry={login.refetch} />
      ) : shown ? (
        <SessionPanel
          session={shown}
          display={data!.providers.find((p) => p.name === shown.provider)?.display ?? shown.provider}
          onDone={() => setWatching(null)}
        />
      ) : (
        <div className="stack">
          {data!.busy && (
            <div className="notice">
              <Icon name="alert" size={18} />
              <span>{t.login.busy}</span>
            </div>
          )}
          {data!.providers.length === 0 && (
            <div className="card pad" style={{ color: 'var(--text-2)', fontSize: 'var(--fs-small)' }}>
              {t.login.noneNeeded}
            </div>
          )}
          {data!.providers.map((p) => {
            const pending = begin.isPending && begin.variables.provider === p.name
            return (
              <div className="card" key={p.name}>
                <div className="cell">
                  <div className="cell-main">
                    <div className="cell-title">{p.display}</div>
                    <div className="cell-sub">{p.available ? t.login.valid : t.login.invalid}</div>
                  </div>
                  <span className={`badge ${p.available ? 'ok' : 'danger'}`}>
                    <i className="dot" />
                    {p.available ? t.common.loggedIn : t.common.expired}
                  </span>
                </div>
                <div className="btn-row" style={{ padding: '4px 14px 14px' }}>
                  <button
                    className="btn primary"
                    disabled={data!.busy || begin.isPending}
                    onClick={() => begin.mutate({ provider: p.name, mode: '' })}
                  >
                    {pending && begin.variables.mode === '' && <span className="spinner" />}
                    {t.login.start}
                  </button>
                  <button
                    className="btn"
                    disabled={data!.busy || begin.isPending}
                    onClick={() => begin.mutate({ provider: p.name, mode: 'token' })}
                  >
                    {pending && begin.variables.mode === 'token' && <span className="spinner" />}
                    {t.login.withToken}
                  </button>
                </div>
              </div>
            )
          })}
          <p className="section-note">
            {t.login.note}
          </p>
        </div>
      )}
    </div>
  )
}

function SessionPanel({ session, display, onDone }: { session: LoginSession; display: string; onDone: () => void }) {
  const client = useQueryClient()
  const remain = useCountdown(session.expires_at)
  const cancel = useMutation({
    mutationFn: () => del('/admin/login'),
    onSuccess: () => client.invalidateQueries({ queryKey: ['admin', 'login'] }),
    onError: (error) => toast.error(error),
  })
  const cancelButton = (
    <button className="btn danger" disabled={cancel.isPending} onClick={() => cancel.mutate()}>
      {t.login.cancel}
    </button>
  )

  if (!active(session)) {
    const ok = session.state === 'success'
    return (
      <div className="stack">
        <div className={`notice ${ok ? 'ok' : 'danger'}`}>
          <Icon name={ok ? 'check' : 'alert'} size={18} />
          <span>
            {ok ? t.login.success(display, session.nickname) : t.login.results[session.state]}
            {session.note && <span style={{ whiteSpace: 'pre-line' }}>{session.note}</span>}
          </span>
        </div>
        <button className="btn" onClick={onDone}>
          {t.login.done}
        </button>
      </div>
    )
  }

  if (session.type === 'token') return <TokenForm session={session} display={display} cancelButton={cancelButton} />

  const scanned = session.state === 'scanned'
  return (
    <div className="stack">
      <div className="card qr-box">
        {session.type === 'qr' ? (
          <img src={session.qr} alt={t.login.qrAlt(display)} className={scanned ? 'dim' : ''} />
        ) : (
          <button className="btn primary" onClick={() => openLink(session.content)}>
            <Icon name="external" size={18} />
            {t.login.openLink}
          </button>
        )}
        <div>
          <div className="qr-state">
            {scanned
              ? t.login.scanned(session.nickname)
              : session.type === 'qr'
                ? t.login.scanWith(display)
                : t.login.finishInPage}
          </div>
          <div className="qr-hint num">{scanned ? t.login.confirmOnPhone : t.login.remaining(remain)}</div>
        </div>
      </div>
      {cancelButton}
    </div>
  )
}

function TokenForm({
  session,
  display,
  cancelButton,
}: {
  session: LoginSession
  display: string
  cancelButton: React.ReactNode
}) {
  const client = useQueryClient()
  const [token, setToken] = useState('')
  const submit = useMutation({
    mutationFn: (value: string) => post('/admin/login/token', { token: value }),
    onSuccess: () => client.invalidateQueries({ queryKey: ['admin', 'login'] }),
    onError: (error) => toast.error(error),
  })
  const remain = useCountdown(session.expires_at)
  const value = token.trim()
  const { mutate } = submit
  const onSubmit = useCallback(() => mutate(value), [mutate, value])
  // 提交后会话仍处于等待状态直到校验结束，期间保持加载
  useMainButton({ text: t.login.submit, onClick: onSubmit, disabled: value === '', loading: submit.isPending || submit.isSuccess })

  // 插件给出的提示是 Telegram HTML，去掉标签后显示
  const prompt = session.content.replace(/<[^>]+>/g, '')
  return (
    <div className="stack">
      <div className="card pad stack">
        <div className="cell-title">{display}</div>
        {prompt && (
          <div className="qr-hint" style={{ whiteSpace: 'pre-line' }}>
            {prompt}
          </div>
        )}
        <textarea
          className="textarea mono"
          placeholder={t.login.tokenPlaceholder}
          autoCapitalize="off"
          autoCorrect="off"
          spellCheck={false}
          value={token}
          onChange={(e) => setToken(e.target.value)}
        />
        <div className="qr-hint num">{t.login.submitWithin(remain)}</div>
      </div>
      {cancelButton}
    </div>
  )
}
