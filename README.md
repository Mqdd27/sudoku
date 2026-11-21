# Sudoku

Classic 9×9 Sudoku in the browser. Four difficulty levels, notes, hints, undo, light and dark themes. Installable and playable offline.

Built with plain TypeScript, HTML and CSS. No runtime dependencies; the production build is static files only.

## Getting started

```sh
cd web
npm install
npm run dev        # http://localhost:5173
npm test           # unit tests (Vitest)
npm run build      # typecheck + production build to web/dist/
npm run preview    # serve the build locally (the service worker only runs here)
```

Difficulty ranges (number of pre-filled cells) and generator time limits live in `web/src/config.ts`.

## Project structure

```
web/
  src/config.ts          difficulty levels, timeouts
  src/logic/sudoku.ts    solver, puzzle generator, conflict check
  src/logic/game.ts      board state and undo history
  src/source.ts          GameSource interface and the local implementation
  src/storage.ts         localStorage schema (keys: sudoku:v1:*)
  src/worker.ts          puzzle generation off the main thread
  src/main.ts            UI and input handling
  src/style.css          layout and themes
  public/sw.js           service worker (offline cache; /api/* and /ws are never cached)
  test/fixtures/         puzzle + solution test vectors (plain JSON)
```

The UI never reads a solution directly. It asks a `GameSource` to check cells, give hints and validate a finished board, so a server-backed source can replace `LocalGameSource` in `src/main.ts` without changing the board code.

## Deployment

`vercel.json` at the repo root builds `web/` and sets cache headers. Import the repo in Vercel, or run `npx vercel --prod` from the repo root.

For a custom domain behind Cloudflare, add it under Vercel → Domains and create a DNS-only (grey cloud) CNAME to `cname.vercel-dns.com`.
