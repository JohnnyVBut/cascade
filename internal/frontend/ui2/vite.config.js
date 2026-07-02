import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Assets are referenced with RELATIVE URLs (base './') so the app works no
// matter what path prefix it is served under. In production the app sits behind
// Caddy's hidden admin path at /<ADMIN_PATH>/ui2/, which is not known at build
// time — a relative base resolves assets against the document location and
// therefore picks up the prefix automatically. (The legacy UI relies on the
// same relative-path trick.)
export default defineConfig({
  base: './',
  plugins: [vue()],
  server: {
    // Local dev: proxy API calls to the running Go backend so `npm run dev`
    // works without CORS and without rebuilding the Go binary on every change.
    proxy: {
      '/api': {
        target: 'http://localhost:8888',
        changeOrigin: true,
      },
    },
  },
  build: {
    // Vite default is dist/; kept as-is. embed_ui2.go embeds ui2/dist.
    outDir: 'dist',
    emptyOutDir: true,
  },
  test: {
    environment: 'node',
    include: ['src/**/*.test.js'],
  },
})
