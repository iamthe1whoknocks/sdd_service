import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Dev: Vite на :5173 проксирует /api и /health на локальный Go API (:8080).
// Prod: статика раздаётся nginx'ом, который проксирует те же пути на app:8080.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
      '/health': 'http://localhost:8080',
    },
  },
})