#!/usr/bin/env bash
# Build the web app and a Linux binary here, copy both to the server, restart the service.
# Usage: deploy/deploy.sh            (nginx serves web/, the binary serves /api and /ws)
#        EMBED=1 deploy/deploy.sh    (the binary also serves the web app; run it with -web)
set -euo pipefail
HOST="${HOST:-mqdd@100.115.174.40}"
DIR="${DIR:-/opt/sudoku}"
GOARCH="${GOARCH:-amd64}"
cd "$(dirname "$0")/.."

npm ci --prefix web
npm run build --prefix web
tags=""
if [ "${EMBED:-}" = 1 ]; then
  rm -rf server/webdist && cp -r web/dist server/webdist
  tags="-tags embedweb"
fi
mkdir -p build
GOOS=linux GOARCH="$GOARCH" CGO_ENABLED=0 go build $tags -trimpath -ldflags "-s -w" -o build/sudoku-server ./server/cmd/sudoku-server

rsync -az --delete web/dist/ "$HOST:$DIR/web/"
rsync -az build/sudoku-server deploy/backup.sh "$HOST:$DIR/"
ssh -t "$HOST" "sudo systemctl restart sudoku && systemctl --no-pager status sudoku | head -5"
