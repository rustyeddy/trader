import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
  plugins: [sveltekit()],
  server: {
    // In dev, forward API calls to the running trader daemon.
    proxy: {
      '/api': 'http://localhost:9999',
      '/health': 'http://localhost:9999',
    },
  },
});
