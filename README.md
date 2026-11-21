# Sudoku

Sudoku in the browser on 4×4, 6×6 and 9×9 boards. Classic mode generates puzzles on the device and plays offline. With an account there is also a Story Mode (40 fixed levels with stars) and PvP, ranked or in private rooms: a Race to finish the same puzzle first, or Time Attack, where the most finished boxes when the clock runs out wins. Mistakes never end a game.

- **Frontend:** plain TypeScript, HTML and CSS built with Vite. No runtime dependencies.
- **Backend:** one Go binary with SQLite (`modernc.org/sqlite`, no CGO). It listens on 127.0.0.1 only, behind nginx.

## Development

You need Node 20+ and Go 1.22+.

```sh
# terminal 1: API + WebSocket on 127.0.0.1:8095 (local data in ./data)
COOKIE_SECURE=false go run ./server

# terminal 2: web app on http://localhost:5173 (proxies /api and /ws to the server)
cd web && npm install && npm run dev
```

Classic mode works without the server. Story, PvP, accounts and the leaderboard need it.

### Tests

```sh
go vet ./server/... ./shared/...
go test -race ./server/... ./shared/...   # solver/generator, auth, Story, PvP over real WebSockets
cd web && npm test                         # solver/generator, game state, local source
```

Both test suites read the same puzzle vectors in `shared/fixtures/`.

### Build

```sh
cd web && npm run build                           # web/dist/
go build -trimpath -ldflags "-s -w" -o sudoku-server ./server
```

To serve the web app from the binary too (without nginx serving files), copy `web/dist` to `server/webdist`, build with `-tags embedweb`, and run with `-web`.

## Configuration

Environment variables, or a `.env` file in the working directory (see `.env.example`):

| Variable | Default | |
|---|---|---|
| `PORT` | `8095` | Port on 127.0.0.1 |
| `DB_PATH` | `data/sudoku.db` | SQLite file (WAL mode). Migrations run on start. |
| `COOKIE_SECURE` | `true` | `false` only for local http |
| `ALLOWED_ORIGINS` | empty | Extra origins for writes and `/ws` |
| `TRUSTED_PROXIES` | `127.0.0.1/32,::1/128` | Peers whose `CF-Connecting-IP`/`X-Real-IP` are trusted |
| `ENV_FILE` | `.env` | Where to read the env file from |

Board sizes, givens per level, the PvP hint setting and Time Attack durations, cooldown and late-move allowance are in `shared/config.json`. Both the web app and the server read this file.

Story levels are in `server/story/levels.json`. They come from a fixed seed, so regenerating them gives the same file: `cd server && go run ./cmd/genlevels`. To add a chapter, append it in `cmd/genlevels/main.go` and run the command again. Existing levels stay the same.

## Project structure

```
shared/            config.json + test fixtures, used by the web app and the Go server
web/src/
  logic/           solver, generator, game state (no DOM)
  board.ts         the game screen, shared by every mode and board size
  source.ts        GameSource interface + LocalGameSource (Classic)
  story.ts         Story map + StorySource (server-judged)
  pvp.ts           PvP pages + WebSocket client
  account.ts       sign in, profile, devices, leaderboard
server/
  sudoku/          solver and generator for any box shape
  migrations/      numbered SQL files, embedded and applied on start
  auth.go          argon2id passwords, hashed session tokens, rate limits
  story.go pvp.go ta.go stats.go
  cmd/genlevels/   builds story/levels.json
docs/protocol.md   PvP WebSocket messages
deploy/            systemd unit, nginx site, backup + deploy scripts, optional Dockerfile
```

The board never reads a solution. It asks a `GameSource` to check cells, give hints and judge a finished board. In Story and PvP the source is the server, so the client never receives the answer.

## Deploying to a server

The intended setup is Ubuntu with nginx in front, Cloudflare for DNS/TLS, and systemd running the binary.

1. One-time setup on the server:
   ```sh
   sudo useradd --system --home /var/lib/sudoku --shell /usr/sbin/nologin sudoku
   sudo install -d -o sudoku -g sudoku -m 750 /var/lib/sudoku
   sudo install -d -o $USER /opt/sudoku
   sudo cp deploy/sudoku.service /etc/systemd/system/ && sudo systemctl daemon-reload && sudo systemctl enable sudoku
   sudo cp deploy/nginx.conf /etc/nginx/sites-available/sudoku   # set server_name and certificate paths
   sudo ln -s /etc/nginx/sites-available/sudoku /etc/nginx/sites-enabled/ && sudo nginx -t && sudo systemctl reload nginx
   ```
2. From your machine: `deploy/deploy.sh`. It builds the web app and a static Linux binary, rsyncs them to `/opt/sudoku`, and restarts the service. Set `HOST` and `DIR` to change the target.
3. Backups: install `sqlite3`, then add `15 3 * * * /opt/sudoku/backup.sh` to the crontab. `BACKUP_DIR` and `KEEP_DAYS` (default 14) set where copies go and how long they're kept.

`CF-Connecting-IP` is only trusted from nginx on localhost. If the origin is also reachable without going through Cloudflare, allow only Cloudflare's IP ranges in the firewall so clients can't fake that header.

### Vercel

`vercel.json` still deploys `web/` on its own. Only Classic mode works that way, because Vercel can't run the Go server. For accounts, Story and PvP, serve the site from your own server as described above.
