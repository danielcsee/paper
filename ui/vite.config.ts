import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Paths owned by FastAPI. Anchored patterns, not bare prefixes: Vite treats a
// string key as a prefix, so '/corpus' would also swallow the UI route
// '/my-corpus'. The pattern is matched against path *and* query, so '\?' must
// be an accepted terminator or '/corpus?page=1' silently misses the proxy and
// the browser gets index.html where it expected JSON.
const API_PATTERNS = [
  '^/pb(/|\\?|$)',
  '^/pm(/|\\?|$)',
  '^/import(/|\\?|$)',
  '^/corpus(/|\\?|$)',
  '^/api(/|\\?|$)',
]

const target = `http://127.0.0.1:${process.env.API_PORT ?? 8000}`

// In dev the UI is served by Vite on 5173 and proxies the API to FastAPI.
// In production `npm run build` emits ./dist, which FastAPI serves directly,
// so these paths are same-origin and no proxy is involved.
export default defineConfig({
  plugins: [react()],
  server: {
    port: Number(process.env.UI_PORT ?? 5173),
    strictPort: true,
    proxy: Object.fromEntries(
      API_PATTERNS.map((pattern) => [pattern, { target, changeOrigin: true }]),
    ),
  },
  build: { outDir: 'dist', sourcemap: true },
})
