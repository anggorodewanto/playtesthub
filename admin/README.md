# playtesthub admin (Extend App UI)

React 19 + TypeScript + Vite Module Federation remote. Rendered inside the AGS Admin Portal under **Extend → My Extend Apps → App UI** (Internal Shared Cloud only — PRD §9 R11).

## Local dev

```
npm install
cp .env.local.example .env.local     # first run only; fill in VITE_AB_*
npm run codegen                      # regenerate src/playtesthubapi/ from the backend's /apidocs/api.json
npm run dev                          # Vite on :5173
```

See `docs/engineering.md` §1.2 + §7 for the full template + codegen contract.

## Scripts

- `npm run dev` — Vite dev server with `devProxyPlugin` proxying `/ext-<ns>-<app>` to AGS.
- `npm run build` — `tsc -b && vite build`. Output: `dist/`.
- `npm run codegen` — re-downloads `playtesthub.json` + regenerates `src/playtesthubapi/`. Rerun when proto HTTP annotations change.
- `npm run test` — Vitest + React Testing Library.

## Deploy

- First-time registration: `extend-helper-cli appui create --namespace $AGS_NAMESPACE --name playtesthub`.
- Upload bundle: `extend-helper-cli appui upload --namespace $AGS_NAMESPACE --name playtesthub`.
