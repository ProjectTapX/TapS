import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'node:path'
import { readFileSync } from 'node:fs'

const pkg = JSON.parse(readFileSync(path.resolve(__dirname, 'package.json'), 'utf-8'))

export default defineConfig({
  plugins: [react()],
  define: {
    __APP_VERSION__: JSON.stringify(pkg.version),
  },
  resolve: {
    alias: { '@': path.resolve(__dirname, 'src') },
  },
  build: {
    chunkSizeWarningLimit: 1600,
    rollupOptions: {
      output: {
        // Splitting react out of the main bundle has caused load-order issues
        // (e.g. antd referencing createContext before react chunk hydrated).
        // Only split heavy unrelated leaf libs that don't import React at module
        // top-level: xterm and recharts. Everything else stays in vendor/main.
        manualChunks(id) {
          if (id.includes('node_modules')) {
            if (id.includes('@xterm') || id.includes('xterm/')) return 'xterm'
            if (id.includes('recharts') || id.includes('d3-')) return 'charts'
          }
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:24444',
        ws: true,
        changeOrigin: true,
      },
    },
  },
})
