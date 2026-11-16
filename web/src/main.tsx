import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Component, StrictMode, type ReactNode } from 'react'
import { createRoot } from 'react-dom/client'
import { App } from './App'
import { Empty } from './components/ui'
import { ApiError } from './lib/api'
import { initTelegram } from './lib/telegram'
import './styles/app.css'
import { t } from './lib/i18n'

initTelegram()

const client = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 15_000,
      refetchOnWindowFocus: false,
      // 鉴权与参数错误重试没有意义
      retry: (count, error) => !(error instanceof ApiError && error.status >= 400 && error.status < 500) && count < 2,
    },
  },
})

/** 渲染出错时显示原因；Telegram 内没有控制台，白屏无从排查 */
class ErrorBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null }

  static getDerivedStateFromError(error: Error) {
    return { error }
  }

  render() {
    if (!this.state.error) return this.props.children
    return (
      <div className="app">
        <Empty icon="alert" title={t.common.pageError}>
          {this.state.error.message}
        </Empty>
      </div>
    )
  }
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ErrorBoundary>
      <QueryClientProvider client={client}>
        <App />
      </QueryClientProvider>
    </ErrorBoundary>
  </StrictMode>,
)
