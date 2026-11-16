import { useMutation, useQuery } from '@tanstack/react-query'
import { useCallback } from 'react'
import { Icon } from '../../components/Icon'
import { toast } from '../../components/Toast'
import { AuthBadge, CellSkeleton, ErrorState } from '../../components/ui'
import { get, post, type PluginUpdate, type Plugins as PluginsData, type UpdateCheck } from '../../lib/api'
import { useAwaitReload } from '../../lib/hooks'
import { push } from '../../lib/router'
import { confirmDialog, haptic, useMainButton } from '../../lib/telegram'
import { t } from '../../lib/i18n'

const usePlugins = () => useQuery({ queryKey: ['admin', 'plugins'], queryFn: () => get<PluginsData>('/admin/plugins') })

export function Plugins() {
  const plugins = usePlugins()
  const awaitReload = useAwaitReload()
  const check = useMutation({
    mutationFn: () => post<UpdateCheck>('/admin/plugins/check'),
    onSuccess: (result) => (result.updates.length > 0 ? haptic.success() : haptic.tap()),
    onError: (error) => toast.error(error),
  })
  const apply = useMutation({
    mutationFn: (names: string[]) =>
      post<{ results: PluginUpdate[]; reloading: boolean }>('/admin/plugins/update', { names }),
    onSuccess: (result) => {
      check.reset()
      if (result.reloading) awaitReload()
    },
    onError: (error) => toast.error(error),
  })

  const updates = check.data?.updates ?? []
  const { mutate } = apply
  const onApply = useCallback(async () => {
    if (await confirmDialog(t.plugins.confirmUpdate(updates.length))) {
      mutate(updates.map((u) => u.name))
    }
  }, [mutate, updates])
  useMainButton(
    updates.length > 0 ? { text: t.plugins.updateButton(updates.length), onClick: onApply, loading: apply.isPending } : null,
  )

  if (plugins.isPending) {
    return (
      <div className="page sub">
        <h1 className="large-title">{t.plugins.title}</h1>
        <CellSkeleton rows={3} />
      </div>
    )
  }
  if (plugins.isError) {
    return (
      <div className="page sub">
        <ErrorState error={plugins.error} onRetry={plugins.refetch} />
      </div>
    )
  }
  const data = plugins.data
  const checkable = data.plugins.some((p) => p.updatable)

  return (
    <div className="page sub">
      <h1 className="large-title">{t.plugins.title}</h1>
      <div className="stack">
        {data.rollbacks.map((rb) => (
          <div className="notice" key={rb.name}>
            <Icon name="alert" size={18} />
            <span className="selectable">
              {t.plugins.rolledBack(rb.display_name, rb.version)}
              <br />
              {rb.error}
            </span>
          </div>
        ))}
        <div className="card">
          {data.plugins.map((p) => (
            <button className="cell" key={p.name} onClick={() => push(`/admin/plugins/${p.name}`)}>
              <span className="cell-main">
                <span className="cell-title truncate" style={{ display: 'block' }}>
                  {p.display_name}
                </span>
                <span className="cell-sub truncate num" style={{ display: 'block' }}>
                  {p.version}
                </span>
              </span>
              <span className="cell-side inline">
                {p.default && <span className="badge accent">{t.common.default}</span>}
                {p.auth === 'expired' && <AuthBadge auth="expired" />}
                <span className="chevron">
                  <Icon name="chevron" size={18} />
                </span>
              </span>
            </button>
          ))}
        </div>
      </div>

      <h2 className="section-title">{t.plugins.updates}</h2>
      <div className="stack">
        {check.data &&
          (check.data.checked === 0 ? (
            <div className="notice">
              <Icon name="alert" size={18} />
              <span>{t.plugins.noneUpdatable}</span>
            </div>
          ) : updates.length === 0 && !check.data.error ? (
            <div className="notice ok">
              <Icon name="check" size={18} />
              <span>{t.plugins.upToDate}</span>
            </div>
          ) : null)}
        {check.data?.error && (
          <div className="notice danger">
            <Icon name="alert" size={18} />
            <span className="selectable">{t.plugins.checkError(check.data.error)}</span>
          </div>
        )}
        {updates.length > 0 && (
          <dl className="card">
            {updates.map((u) => (
              <div className="kv" key={u.name}>
                <dt>{u.display_name}</dt>
                <dd className="num">
                  {u.from} → {u.to}
                </dd>
              </div>
            ))}
          </dl>
        )}
        {apply.data && (
          <dl className="card">
            {apply.data.results.map((r) => (
              <div className="kv" key={r.name}>
                <dt>{r.display_name}</dt>
                <dd className="num" style={r.error ? { color: 'var(--danger-text)' } : undefined}>
                  {r.error ?? t.plugins.updatedTo(r.to)}
                </dd>
              </div>
            ))}
          </dl>
        )}
        <button className="btn" disabled={!checkable || check.isPending || apply.isPending} onClick={() => check.mutate()}>
          {check.isPending ? <span className="spinner" /> : <Icon name="refresh" size={18} />}
          {t.plugins.check}
        </button>
        <p className="section-note" style={{ marginTop: 0 }}>
          {checkable
            ? t.plugins.checkNote
            : t.plugins.noUpdateUrl}
        </p>
      </div>
    </div>
  )
}

export function PluginDetail({ name }: { name: string }) {
  const plugins = usePlugins()
  const manifest = useQuery({
    queryKey: ['admin', 'plugins', name, 'manifest'],
    queryFn: () => get<{ manifest: unknown }>(`/admin/plugins/${name}/manifest`),
  })
  const p = plugins.data?.plugins.find((x) => x.name === name)

  return (
    <div className="page sub">
      <h1 className="large-title truncate">{p?.display_name ?? name}</h1>
      {p && (
        <dl className="card">
          <div className="kv">
            <dt>{t.plugins.name}</dt>
            <dd className="mono selectable">{p.name}</dd>
          </div>
          <div className="kv">
            <dt>{t.plugins.version}</dt>
            <dd className="num selectable">{p.version}</dd>
          </div>
          {p.build && (
            <div className="kv">
              <dt>{t.plugins.build}</dt>
              <dd className="num selectable">{p.build}</dd>
            </div>
          )}
          <div className="kv">
            <dt>{t.plugins.capabilities}</dt>
            <dd>{t.common.list(p.capabilities) || t.plugins.none}</dd>
          </div>
          <div className="kv">
            <dt>{t.plugins.instances}</dt>
            <dd className="num">{p.instances}</dd>
          </div>
          <div className="kv">
            <dt>{t.plugins.autoUpdate}</dt>
            <dd>{p.updatable ? t.plugins.supported : t.plugins.noUpdateDeclared}</dd>
          </div>
          <div className="kv">
            <dt>{t.plugins.file}</dt>
            <dd className="mono selectable">{p.path}</dd>
          </div>
        </dl>
      )}
      <h2 className="section-title">Manifest</h2>
      {manifest.isPending ? (
        <CellSkeleton rows={4} side={false} />
      ) : manifest.isError ? (
        <ErrorState error={manifest.error} onRetry={manifest.refetch} />
      ) : (
        <div className="card">
          <pre className="code-block">{JSON.stringify(manifest.data.manifest, null, 2)}</pre>
        </div>
      )}
    </div>
  )
}
