#!/usr/bin/env bash
# Daily SQLite backup. Cron (as root or the sudoku user):
#   15 3 * * * /opt/sudoku/backup.sh
# VACUUM INTO gives a consistent copy while the server keeps running (WAL mode).
set -euo pipefail
DB="${DB:-/var/lib/sudoku/sudoku.db}"
BACKUP_DIR="${BACKUP_DIR:-/var/backups/sudoku}"   # keep this outside the data folder
KEEP_DAYS="${KEEP_DAYS:-14}"
SQLITE="${SQLITE:-sqlite3}"

mkdir -p "$BACKUP_DIR"
out="$BACKUP_DIR/sudoku-$(date +%Y%m%d-%H%M%S).db"
"$SQLITE" "$DB" "VACUUM INTO '$out'"
gzip "$out"
find "$BACKUP_DIR" -name 'sudoku-*.db.gz' -mtime +"$KEEP_DAYS" -delete
echo "backup: $out.gz"
