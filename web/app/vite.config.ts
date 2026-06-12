/// <reference types="vitest/config" />
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import UnoCSS from 'unocss/vite'

const API_TARGET = process.env.PATHCRAFT_API ?? 'http://localhost:8080'
const API_PATHS = [
  '/config',
  '/modes',
  '/route',
  '/journey',
  '/nearest',
  '/graph',
  '/nodes',
  '/transit',
  '/health',
  '/status',
]

export default defineConfig({
  plugins: [UnoCSS(), react()],
  server: {
    proxy: Object.fromEntries(API_PATHS.map((path) => [path, API_TARGET])),
  },
  test: {
    environment: 'node',
  },
})
