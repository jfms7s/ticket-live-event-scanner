import react from '@vitejs/plugin-react';
import { defineConfig } from 'vitest/config';

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: 'dist',
    // dist/.gitkeep is tracked so the Go embed compiles before a build.
    emptyOutDir: false,
  },
  server: {
    // `npm run dev` talks to a local web-ui-api, as in production where the
    // dashboard and the API share an origin.
    proxy: { '/api': 'http://localhost:8080' },
  },
  test: {
    environment: 'node',
  },
});
