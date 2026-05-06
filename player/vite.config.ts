import { defineConfig, loadEnv } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';

// Dev-only affordance: when VITE_BACKEND_URL is set (e.g. in player/.env)
// the dev server proxies every request starting with VITE_BACKEND_BASE_PATH
// (default "/playtesthub") to that backend. Lets config.json carry a
// same-origin `grpcGatewayUrl` and sidesteps the cross-origin browser
// policy without requiring CORS on the backend for local work.
//
// In production builds the proxy config is irrelevant — `npm run build`
// produces a static bundle with no dev server.
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '');
  const backend = env.VITE_BACKEND_URL;
  const basePath = env.VITE_BACKEND_BASE_PATH || '/playtesthub';
  // GitHub Pages serves the bundle under `/<repo>/` — set VITE_BASE to that
  // subpath at build time. Empty/unset → root-served deploy (Vercel, custom
  // domain, or local `npm run dev`). Trailing slash is required by Vite +
  // BASE_URL consumers (loadConfig, bridgePathCallback).
  const base = env.VITE_BASE || '/';

  return {
    base,
    plugins: [svelte(), tailwindcss()],
    build: {
      outDir: 'dist',
      sourcemap: true,
    },
    server: {
      port: 5173,
      proxy: backend
        ? {
            [basePath]: { target: backend, changeOrigin: true },
          }
        : undefined,
    },
  };
});
