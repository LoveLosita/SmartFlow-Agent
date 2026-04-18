import { fileURLToPath, URL } from 'node:url'

import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

// 1. 开发环境把 /api 代理到本地 Go 服务，避免前端先处理跨域。
// 2. 这里只负责联调体验，不负责生产环境网关配置。
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    host: '0.0.0.0',
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
        // SSE 流必须禁用缓冲，否则 Vite proxy 会攒满 buffer 再转发，
        // 导致前端长时间收不到数据被判定为连接中断。
        configure: (proxy) => {
          proxy.on('proxyRes', (proxyRes) => {
            proxyRes.headers['x-accel-buffering'] = 'no'
            proxyRes.headers['cache-control'] = 'no-cache'
          })
        },
      },
    },
  },
})
