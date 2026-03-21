#!/bin/sh
# docker-entrypoint.sh — import fafb workflows then start n8n
#
# Workflows are imported once on first run. The sentinel file at
# /home/node/.n8n/.caboose-imported prevents re-importing on restarts.

set -e

SENTINEL="/home/node/.n8n/.caboose-imported"

if [ ! -f "$SENTINEL" ]; then
  echo "[caboose-n8n] importing workflows..."
  for f in /caboose-workflows/*.json; do
    echo "[caboose-n8n]   importing $f"
    n8n import:workflow --input="$f" || echo "[caboose-n8n]   WARNING: failed to import $f (continuing)"
  done
  touch "$SENTINEL"
  echo "[caboose-n8n] workflows imported. Activate them in the n8n UI."
else
  echo "[caboose-n8n] workflows already imported, skipping."
fi

# Hand off to the standard n8n entrypoint
exec n8n start
