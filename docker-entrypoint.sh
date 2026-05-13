#!/bin/sh
set -e

DATA_DIR="/pb/main/pb_data"

# Garantisce che il volume montato sia di proprieta` dell'utente del processo,
# anche se e` stato popolato con owner/perms diversi (deploy precedenti come root,
# restore da backup, copy via docker cp, etc.). Risolve "attempt to write a
# readonly database" causato da hot journal/WAL non rollbackabili per permessi.
mkdir -p "$DATA_DIR"
chown -R app:app "$DATA_DIR"
find "$DATA_DIR" -type f -name 'data.db*' -exec chmod u+rw {} +

exec su-exec app "$@"
