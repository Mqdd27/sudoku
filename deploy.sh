#!/usr/bin/env bash
# Build, test, and rsync dist/ to the server. Override with env vars:
#   REMOTE=user@host WEB_DIR=/opt/sudoku ./deploy.sh
set -euo pipefail
cd "$(dirname "$0")"

REMOTE="${REMOTE:-mqdd@100.115.174.40}"
WEB_DIR="${WEB_DIR:-/opt/sudoku}"

npm test
npm run build
# --delay-updates: swap files in at the end so visitors never see a half-uploaded build.
rsync -az --delete --delay-updates --chmod=Du=rwx,Dgo=rx,Fu=rw,Fgo=r dist/ "$REMOTE:$WEB_DIR/"
echo "Deployed to $REMOTE:$WEB_DIR"
