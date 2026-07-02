import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// The UI2 app is served from the Go binary under the /ui2/ path prefix, so
// asset URLs must be prefixed accordingly. Vue Router uses the same base.
export default defineConfig({
  base: '/ui2/',
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
