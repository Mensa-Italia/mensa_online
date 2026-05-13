#!/bin/sh
set -e

DATA_DIR="/pb/main/pb_data"
LOG_DIR="$DATA_DIR/logs"
LOG_FILE="$LOG_DIR/app.log"

# Garantisce che il volume montato sia di proprieta` dell'utente del processo,
# anche se e` stato popolato con owner/perms diversi (deploy precedenti come root,
# restore da backup, copy via docker cp, etc.). Risolve "attempt to write a
# readonly database" causato da hot journal/WAL non rollbackabili per permessi.
mkdir -p "$DATA_DIR" "$LOG_DIR"
chown -R app:app "$DATA_DIR"
find "$DATA_DIR" -type f -name 'data.db*' -exec chmod u+rw {} +

# Rotazione semplice: se il log corrente supera 50MB, archivia e ricomincia.
if [ -f "$LOG_FILE" ] && [ "$(stat -c%s "$LOG_FILE" 2>/dev/null || stat -f%z "$LOG_FILE")" -gt 52428800 ]; then
    mv "$LOG_FILE" "$LOG_FILE.1"
fi

# Lancia l'app come user `app` e duplica stdout+stderr su file persistente in
# pb_data/logs/app.log accessibile via shell del container (`tail -F`) anche se
# `docker logs` non e` raggiungibile (es. Portainer senza permessi log API).
export LOG_FILE
exec su-exec app sh -c 'exec "$@" 2>&1 | tee -a "$LOG_FILE"' sh "$@"
