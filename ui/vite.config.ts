import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// In dev the UI is served by Vite on 5173 and proxies /api to FastAPI on 8000.
// In production `npm run build` emits ./dist, which FastAPI serves directly.
export default defineConfig({
  plugins: [react()],
  server: {
    port: Number(process.env.UI_PORT ?? 5173),
    strictPort: true,
    proxy: {
      '/api': {
        target: `http://127.0.0.1:${process.env.API_PORT ?? 8000}`,
        changeOrigin: true,
      },
    },
  },
  build: { outDir: 'dist', sourcemap: true },
})
