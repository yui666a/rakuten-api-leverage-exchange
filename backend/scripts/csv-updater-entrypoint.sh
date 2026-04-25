#!/usr/bin/env bash
# Entry point for the csv-updater container.
#  - Optionally runs the fetch script once on startup (RUN_ON_START=1, default)
#    so a freshly started container catches up before waiting for the next cron
#    tick.
#  - Then hands off to crond running in the foreground.

set -euo pipefail

if [[ "${RUN_ON_START:-1}" == "1" ]]; then
  echo "[csv-updater] running initial fetch on startup..."
  /app/backend/scripts/fetch_incremental.sh || echo "[csv-updater] initial fetch failed (continuing)"
fi

echo "[csv-updater] starting crond (foreground)"
exec crond -f -l 8
