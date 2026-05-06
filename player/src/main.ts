import { mount } from 'svelte';
import './app.css';
import App from './App.svelte';
import BootError from './routes/BootError.svelte';
import { bridgePathCallback } from './lib/bootstrap';
import { ConfigError, loadConfig } from './lib/config';

bridgePathCallback();

const target = document.getElementById('app');
if (!target) throw new Error('playtesthub: #app mount point missing from index.html');

// Default config.json lives next to the bundle — under the Vite
// compile-time base. `VITE_CONFIG_URL` is still honoured for builds
// that want to point at an external host.
const configUrl = import.meta.env.VITE_CONFIG_URL ?? `${import.meta.env.BASE_URL}config.json`;

loadConfig(configUrl).then(
  (config) => {
    mount(App, { target, props: { config } });
  },
  (err) => {
    const message =
      err instanceof ConfigError ? err.message : `Unexpected error: ${(err as Error).message}`;
    mount(BootError, { target, props: { message } });
  },
);
