import react from '@vitejs/plugin-react'
import { defineConfig, loadEnv } from 'vite'

// 构建产物输出到 internal/webapp/dist，由 go:embed 打进二进制
export default defineConfig(({ mode }) => {
  // 本地联调：VITE_API=http://127.0.0.1:8080 npm run dev
  const api = loadEnv(mode, '.', 'VITE_').VITE_API
  return {
    plugins: [react()],
    base: './',
    build: {
      outDir: '../internal/webapp/dist',
      emptyOutDir: true,
      target: 'es2020',
    },
    server: {
      proxy: api ? { '/api': api } : undefined,
    },
  }
})
