import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The dev server proxies /api server-side, so the local UI can run against
// staging or production by changing one env var and CORS stays out of it.
const proxyTarget = process.env.VITE_PROXY_TARGET || 'http://localhost:8088'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5176,
    proxy: {
      '/api': { target: proxyTarget, changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
  },
})
