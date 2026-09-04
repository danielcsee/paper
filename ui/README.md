# ui

The React + TypeScript frontend: a chat window for asking questions about the
literature, beside a sidebar for searching papers directly.

Built with Vite. Start it through [`scripts/dev.sh`](../scripts) rather than on
its own, so the API it proxies to is running too.

```bash
npm --prefix ui run dev        # Vite dev server on :5173
npm --prefix ui run build      # type-check, then emit dist/
npm --prefix ui run typecheck  # tsc, no output
```

## How it reaches the API

In development Vite serves the UI on `5173` and proxies `/pb`, `/import`, `/corpus` and `/api` to
FastAPI on `8000` (see `vite.config.ts`; both ports honour `UI_PORT` and
`API_PORT`). In production `npm run build` emits `dist/`, which FastAPI serves
itself — so those paths are same-origin and no proxy is involved. Fetches use
relative URLs for exactly this reason.

## Subdirectories

- [`src/`](src) — application source
- `dist/` — build output, generated and git-ignored

## Dependencies

`react` and `react-dom` 19 at runtime. Dev: `vite`, `@vitejs/plugin-react`,
`typescript`, and the `@types/*` packages.

No router, state manager, or component library — the app is a single page and
does not need them yet.

## Notes

The chat pane is a shell: `App.tsx` answers every message with a fixed
placeholder, because the retrieval pipeline is not wired up. The sidebar search
is real and calls `/pb/search`.
