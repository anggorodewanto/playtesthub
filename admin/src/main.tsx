import './index.css'
import { module } from './module.tsx'

if (import.meta.env.DEV) {
  // Dev mode only, we want to reset preflight to make it as same as Admin Portal as possible.
  import('./devmode.css')
}

const baseURL = window.location.origin

module.mount(document.getElementById('root')!, {
  basePath: import.meta.env.VITE_BASE_PATH || '/',
  sdkConfig: {
    baseURL,
    clientId: import.meta.env.VITE_AB_CLIENT_ID,
    namespace: import.meta.env.VITE_AB_NAMESPACE,
    redirectURI: import.meta.env.VITE_AB_REDIRECT_URI
  }
  // isCurrentUserHasPermission is omitted — appuiContextProvider falls back
  // to the dev-only implementation automatically. In production the Admin Portal
  // host passes this via HostContext.
})
