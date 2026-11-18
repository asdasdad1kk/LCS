import path from 'node:path'
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { '@': path.resolve(__dirname, './src') }
  },
  // El worker de análisis de DWG usa el WASM de LibreDWG (módulo ES).
  worker: { format: 'es' },
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
    chunkSizeWarningLimit: 6000,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (id.includes('node_modules/three')) return 'three'
          if (id.includes('@mlightcad')) return 'cad'
        }
      }
    }
  },
  server: {
    port: 5173,
    // La ventana de Wails carga el front por un proxy HTTP que no reenvía WebSockets:
    // el cliente de HMR se conecta directo a Vite.
    hmr: { protocol: 'ws', host: 'localhost', port: 5173 },
    proxy: {
      '/api': 'http://127.0.0.1:7809'
    }
  }
})
