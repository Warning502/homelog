#!/usr/bin/env bash
# Deploys the latest main branch to a Linux server over SSH as a public demo.
# Run from the repository on your own machine (Git Bash on Windows works):
#
#   bash scripts/deploy-server.sh root@SERVER_IP
#
# It uploads the code of origin/main (no GitHub access needed on the server),
# installs Docker there if missing, and starts the demo on port 80. You are
# asked for the SSH password once; nothing is stored. Re-run to update.
set -euo pipefail

TARGET="${1:-}"
if [ -z "$TARGET" ]; then
  echo "Usage: bash scripts/deploy-server.sh user@server-ip [remote-dir]" >&2
  exit 1
fi
REMOTE_DIR="${2:-/opt/homelog-demo}"

cd "$(git rev-parse --show-toplevel)"
echo "==> Fetching the latest main..."
git fetch -q origin main
echo "==> Deploying $(git rev-parse --short origin/main) to ${TARGET}:${REMOTE_DIR}"

# core.autocrlf=false: on Windows, git archive would otherwise convert every
# text file to CRLF, and the server's bash chokes on "set -o pipefail\r".
# The remote side strips any stray CR from the setup script as a second guard.
git -c core.autocrlf=false archive --format=tar origin/main | ssh -o StrictHostKeyChecking=accept-new "$TARGET" \
  "set -e; mkdir -p '$REMOTE_DIR' && tar -x -C '$REMOTE_DIR' && cd '$REMOTE_DIR' && tr -d '\\r' < scripts/server-setup.sh > .setup.sh && bash .setup.sh; rc=\$?; rm -f .setup.sh; exit \$rc"
