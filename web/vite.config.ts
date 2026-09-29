/// <reference types="vitest" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import path from 'node:path';

// The UI is served by the hub from the same origin; in dev, Vite proxies API calls to it.
export default defineConfig({
  plugins: [react()],
  resolve: { alias: { '@': path.resolve(__dirname, 'src') } },
  server: {
    proxy: {
      '/api': { target: process.env.DTH_API ?? 'http://localhost:8080', changeOrigin: false },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: false,
    chunkSizeWarningLimit: 1500,
    rollupOptions: {
      output: {
        manualChunks: {
          react: ['react', 'react-dom', 'react-router-dom', '@tanstack/react-query'],
          charts: ['recharts'],
          graph: ['cytoscape'],
          markdown: ['react-markdown', 'remark-gfm', 'rehype-sanitize'],
        },
      },
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['tests/setup.ts'],
    css: false,
  },
});
