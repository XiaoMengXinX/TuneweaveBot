import { useMutation, useQuery } from '@tanstack/react-query'
import { useCallback } from 'react'
import { Icon } from '../../components/Icon'
import { toast } from '../../components/Toast'
import { AuthBadge, CellSkeleton, ErrorState } from '../../components/ui'
import { get, post, type PluginUpdate, type Plugins as PluginsData, type UpdateCheck } from '../../lib/api'
import { useAwaitReload } from '../../lib/hooks'
import { push } from '../../lib/router'
import { confirmDialog, haptic, useMainButton } from '../../lib/telegram'

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
    if (await confirmDialog(`更新${updates.length}个插件？更新后bot会在当前任务完成后重载。`)) {
      mutate(updates.map((u) => u.name))
    }
  }, [mutate, updates])
  useMainButton(
    updates.length > 0 ? { text: `更新${updates.length}个插件并重载`, onClick: onApply, loading: apply.isPending } : null,
  )

  if (plugins.isPending) {
    return (
      <div className="page sub">
        <h1 className="large-title">插件</h1>
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
      <h1 className="large-title">插件</h1>
      <div className="stack">
        {data.rollbacks.map((rb) => (
          <div className="notice" key={rb.name}>
            <Icon name="alert" size={18} />
            <span className="selectable">
              {rb.display_name}的新版本加载失败，已回滚到{rb.version}。
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
                {p.default && <span className="badge accent">默认</span>}
                {p.auth === 'expired' && <AuthBadge auth="expired" />}
                <span className="chevron">
                  <Icon name="chevron" size={18} />
                </span>
              </span>
            </button>
          ))}
        </div>
      </div>

      <h2 className="section-title">更新</h2>
      <div className="stack">
        {check.data &&
          (check.data.checked === 0 ? (
            <div className="notice">
              <Icon name="alert" size={18} />
              <span>没有可自动更新的插件。</span>
            </div>
          ) : updates.length === 0 && !check.data.error ? (
            <div className="notice ok">
              <Icon name="check" size={18} />
              <span>所有插件都是最新版本。</span>
            </div>
          ) : null)}
        {check.data?.error && (
          <div className="notice danger">
            <Icon name="alert" size={18} />
            <span className="selectable">检查更新时出错：{check.data.error}</span>
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
                  {r.error ?? `已更新到${r.to}`}
                </dd>
              </div>
            ))}
          </dl>
        )}
        <button className="btn" disabled={!checkable || check.isPending || apply.isPending} onClick={() => check.mutate()}>
          {check.isPending ? <span className="spinner" /> : <Icon name="refresh" size={18} />}
          检查更新
        </button>
        <p className="section-note" style={{ marginTop: 0 }}>
          {checkable
            ? '更新前会校验sha256并备份旧文件。新版本加载失败时自动回滚，并在这里提示。'
            : '已加载的插件没有声明更新地址（本地构建的插件不参与自动更新）。'}
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
            <dt>名称</dt>
            <dd className="mono selectable">{p.name}</dd>
          </div>
          <div className="kv">
            <dt>版本</dt>
            <dd className="num selectable">{p.version}</dd>
          </div>
          {p.build && (
            <div className="kv">
              <dt>构建</dt>
              <dd className="num selectable">{p.build}</dd>
            </div>
          )}
          <div className="kv">
            <dt>能力</dt>
            <dd>{p.capabilities.join('、') || '无'}</dd>
          </div>
          <div className="kv">
            <dt>常驻实例</dt>
            <dd className="num">{p.instances}</dd>
          </div>
          <div className="kv">
            <dt>自动更新</dt>
            <dd>{p.updatable ? '支持' : '未声明更新地址'}</dd>
          </div>
          <div className="kv">
            <dt>文件</dt>
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
