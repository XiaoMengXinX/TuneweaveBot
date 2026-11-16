import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Icon } from '../../components/Icon'
import { toast } from '../../components/Toast'
import { CellSkeleton, ErrorState } from '../../components/ui'
import { del, get, post, type LoginSession, type LoginState } from '../../lib/api'
import { haptic, openLink, useMainButton } from '../../lib/telegram'

const RESULT: Record<string, string> = {
  failed: '登录失败，请稍后重试',
  expired: '登录已过期，请重新发起',
  cancelled: '已取消登录',
  reloading: 'bot正在重载，登录已中断，请稍后重试',
  check_failed: '查询登录状态失败，请稍后重试',
}

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
      <h1 className="large-title">登录</h1>
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
              <span>已有通过 /login 命令发起的登录会话，结束后才能在这里登录。</span>
            </div>
          )}
          {data!.providers.length === 0 && (
            <div className="card pad" style={{ color: 'var(--text-2)', fontSize: 'var(--fs-small)' }}>
              已加载的插件都不需要登录。
            </div>
          )}
          {data!.providers.map((p) => {
            const pending = begin.isPending && begin.variables.provider === p.name
            return (
              <div className="card" key={p.name}>
                <div className="cell">
                  <div className="cell-main">
                    <div className="cell-title">{p.display}</div>
                    <div className="cell-sub">{p.available ? '凭据有效' : '凭据已失效，新歌下载已暂停'}</div>
                  </div>
                  <span className={`badge ${p.available ? 'ok' : 'danger'}`}>
                    <i className="dot" />
                    {p.available ? '已登录' : '已失效'}
                  </span>
                </div>
                <div className="btn-row" style={{ padding: '4px 14px 14px' }}>
                  <button
                    className="btn primary"
                    disabled={data!.busy || begin.isPending}
                    onClick={() => begin.mutate({ provider: p.name, mode: '' })}
                  >
                    {pending && begin.variables.mode === '' && <span className="spinner" />}
                    登录
                  </button>
                  <button
                    className="btn"
                    disabled={data!.busy || begin.isPending}
                    onClick={() => begin.mutate({ provider: p.name, mode: 'token' })}
                  >
                    {pending && begin.variables.mode === 'token' && <span className="spinner" />}
                    用token登录
                  </button>
                </div>
              </div>
            )
          })}
          <p className="section-note">
            登录方式由插件决定：扫码、打开链接或提交token。登录成功后凭据会写回配置文件，被暂停的下载自动恢复。
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
      取消登录
    </button>
  )

  if (!active(session)) {
    const ok = session.state === 'success'
    return (
      <div className="stack">
        <div className={`notice ${ok ? 'ok' : 'danger'}`}>
          <Icon name={ok ? 'check' : 'alert'} size={18} />
          <span>
            {ok ? `${display}登录成功${session.nickname ? `：${session.nickname}` : ''}` : RESULT[session.state]}
            {session.note && <span style={{ whiteSpace: 'pre-line' }}>{session.note}</span>}
          </span>
        </div>
        <button className="btn" onClick={onDone}>
          完成
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
          <img src={session.qr} alt={`${display}登录二维码`} className={scanned ? 'dim' : ''} />
        ) : (
          <button className="btn primary" onClick={() => openLink(session.content)}>
            <Icon name="external" size={18} />
            打开登录链接
          </button>
        )}
        <div>
          <div className="qr-state">
            {scanned
              ? `已扫码${session.nickname ? `：${session.nickname}` : ''}`
              : session.type === 'qr'
                ? `用${display}App扫码`
                : '在打开的页面中完成登录'}
          </div>
          <div className="qr-hint num">{scanned ? '请在手机上确认登录' : `剩余${remain}秒`}</div>
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
  useMainButton({ text: '提交', onClick: onSubmit, disabled: value === '', loading: submit.isPending || submit.isSuccess })

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
          placeholder="粘贴token或cookie"
          autoCapitalize="off"
          autoCorrect="off"
          spellCheck={false}
          value={token}
          onChange={(e) => setToken(e.target.value)}
        />
        <div className="qr-hint num">请在{remain}秒内提交</div>
      </div>
      {cancelButton}
    </div>
  )
}
