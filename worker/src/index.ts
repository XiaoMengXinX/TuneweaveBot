// 看板的 Worker 模式：把全部请求经 Workers VPC 转发给 bot，让没有域名的部署可以用
// *.workers.dev 作为看板地址。页面、接口、身份校验与限流都由 bot 提供；Worker 只在边缘缓存
// 带内容哈希的前端文件，所以升级 bot 后不需要重新部署 Worker。

interface Env {
  BOT: { fetch(request: Request): Promise<Response> }
}

interface Context {
  waitUntil(promise: Promise<unknown>): void
}

interface EdgeCache {
  match(request: Request): Promise<Response | undefined>
  put(request: Request, response: Response): Promise<void>
}

declare const caches: { default: EdgeCache }

function forwardToBot(request: Request, env: Env, url: URL): Promise<Response> {
  // 路由由 VPC Service 的配置决定，URL 中的主机名只作为 Host 头
  const proxied = new Request(`http://bot${url.pathname}${url.search}`, request)
  // bot 按客户端 IP 限流：只传 Cloudflare 给出的地址，丢弃客户端自带的转发头
  proxied.headers.set('X-Real-IP', request.headers.get('CF-Connecting-IP') ?? '')
  proxied.headers.delete('X-Forwarded-For')
  return env.BOT.fetch(proxied)
}

function unavailable(url: URL): Response {
  // tunnel 断开，或 bot 正在重载、尚未启动
  const message = 'bot暂时无法访问，请稍后再试'
  if (url.pathname.startsWith('/api/')) return Response.json({ error: message }, { status: 502 })
  return new Response(message, { status: 502, headers: { 'Content-Type': 'text/plain; charset=utf-8' } })
}

export default {
  async fetch(request: Request, env: Env, ctx: Context): Promise<Response> {
    const url = new URL(request.url)
    // /assets/ 下的文件名带内容哈希，bot 以 immutable 返回，可以长期缓存在边缘
    const cacheable = request.method === 'GET' && url.pathname.startsWith('/assets/')
    if (cacheable) {
      const hit = await caches.default.match(request)
      if (hit) return hit
    }

    let response: Response
    try {
      response = await forwardToBot(request, env, url)
    } catch {
      return unavailable(url)
    }
    if (cacheable && response.status === 200) {
      ctx.waitUntil(caches.default.put(request, response.clone()))
    }
    return response
  },
}
