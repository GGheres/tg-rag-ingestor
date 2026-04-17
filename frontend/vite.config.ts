import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:18080',
      },
      '/youtube-audio-files': {
        target: 'http://localhost:8090',
        rewrite: (path) => path.replace('/youtube-audio-files', '/api/download-youtube-audio-file'),
      },
    },
  },
})
