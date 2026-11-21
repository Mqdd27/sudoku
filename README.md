# Sudoku

No-game-over Sudoku. Vanilla TypeScript, no runtime dependencies, static files only. Works offline (PWA).

## Dev

```sh
npm install
npm run dev        # http://localhost:5173
npm test           # Vitest: solver, generator (uniqueness + givens per level), game state
npm run build      # typecheck + build to dist/
npm run preview    # serve dist/ locally (service worker only runs in the build)
```

Difficulty (given-cell ranges) and generator time limits: `src/config.ts`.

## Layout

```
src/config.ts          levels + timeouts
src/logic/sudoku.ts    solver (bitmask backtracking), generator, conflict validator
src/logic/game.ts      game state, mistakes, hint, undo history
src/worker.ts          runs the generator off the main thread
src/main.ts            UI: board, input, timer, storage, settings
src/style.css          mobile-first layout, light/dark themes
public/sw.js           hand-written service worker (precache list injected at build, see vite.config.ts)
```

## Deploy (Vercel)

`vercel.json` already holds the build settings and cache headers. Either:

- Import the GitHub repo at vercel.com/new (no settings to change), or
- `npx vercel` (preview) / `npx vercel --prod` from this folder.

Custom domain on Cloudflare: add it in Vercel → Domains, then a DNS-only (grey cloud) CNAME to `cname.vercel-dns.com`.
