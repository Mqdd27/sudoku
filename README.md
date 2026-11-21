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

## Deploy

One-time on the server (Ubuntu + nginx):

```sh
sudo mkdir -p /opt/sudoku && sudo chown "$USER": /opt/sudoku
sudo cp nginx.conf /etc/nginx/sites-available/sudoku   # edit server_name first
sudo ln -s /etc/nginx/sites-available/sudoku /etc/nginx/sites-enabled/
sudo nginx -t && sudo systemctl reload nginx
```

Then from your machine, every release:

```sh
./deploy.sh                                     # defaults: mqdd@100.115.174.40:/opt/sudoku
REMOTE=me@host WEB_DIR=/var/www/sudoku ./deploy.sh
```

Point a Cloudflare DNS record (proxied) at the server for the `server_name` you chose.
