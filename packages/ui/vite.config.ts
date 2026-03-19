import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// The app is embedded into the Go server at /ui/.
// Set VITE_BASE_URL env var in dev if proxying through the Go server.
export default defineConfig({
  plugins: [react()],
  base: '/ui/',
  build: {
    outDir: 'dist',
    sourcemap: false,
    rollupOptions: {
      output: {
        manualChunks: {
          react: ['react', 'react-dom', 'react-router-dom'],
          icons: ['lucide-react'],
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      // Proxy sandbox API to Go server in local dev
      '/api': { target: 'http://localhost:8080', changeOrigin: true },
    },
  },
})
