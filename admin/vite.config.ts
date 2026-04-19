import { devProxyPlugin } from '@accelbyte/sdk-extend-app-ui/plugins'
import { federation } from '@module-federation/vite'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig, loadEnv } from 'vite'

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, '.')

  return {
    base: mode === 'production' ? process.env.BASE_URL : '/',
    build: {
      // This has to be set to false to prevent Chrome sending request without a `referer` HTTP header.
      modulePreload: false
    },
    plugins: [
      react(),
      tailwindcss(),
      federation({
        name: 'remote',
        filename: 'remoteEntry.js',
        manifest: true,
        exposes: {
          '.': './src/mf-entry.ts'
        },
        shared: {
          react: {},
          'react-dom': {},
          '@tanstack/react-query': {}
        }
      }),
      devProxyPlugin({ baseUrl: env.VITE_AB_BASE_URL, redirectURI: env.VITE_AB_REDIRECT_URI })
    ]
  }
})
