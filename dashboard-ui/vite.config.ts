import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Dev: `task dashboard-dev` proxies /api to a running `ctx-optimize serve`.
// Build: `task dashboard-build` copies dist/ into internal/dashboard/ui/
// (the go:embed dir — committed, so `go install` never needs node).
export default defineConfig({
  plugins: [react()],
  base: './',
  server: {
    proxy: { '/api': 'http://127.0.0.1:4747' },
  },
  build: {
    outDir: 'dist',
    // One predictable bundle; no external requests ever (CSP-tested Go-side).
    assetsInlineLimit: 8192,
  },
})
