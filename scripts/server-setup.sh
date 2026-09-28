#!/usr/bin/env bash
# Runs ON the server (called by scripts/deploy-server.sh) from the directory
# the code was just unpacked into. Installs Docker if needed, then builds and
# (re)starts the public HomeLog demo. Safe to re-run: it keeps the existing
# .env (and so the same JWT secret).
set -euo pipefail

if [ "$(id -u)" -ne 0 ]; then SUDO=sudo; else SUDO=; fi

if ! command -v docker >/dev/null 2>&1; then
  echo "==> Installing Docker..."
  curl -fsSL https://get.docker.com | $SUDO sh
fi
if ! $SUDO docker compose version >/dev/null 2>&1; then
  echo "❌ Docker Compose plugin is missing. Install docker-compose-plugin and re-run." >&2
  exit 1
fi
$SUDO systemctl enable --now docker >/dev/null 2>&1 || true

PORT="${HOST_PORT:-80}"
if [ -f .env ] && grep -q '^HOST_PORT=' .env; then
  PORT="$(grep '^HOST_PORT=' .env | tail -1 | cut -d= -f2)"
fi
if [ ! -f .env ]; then
  echo "==> Creating .env with a new random JWT secret"
  umask 077
  {
    echo "JWT_SECRET=$(openssl rand -base64 32 2>/dev/null || head -c 32 /dev/urandom | base64)"
    echo "HOST_PORT=${PORT}"
    echo "TZ=Asia/Bangkok"
  } > .env
fi

# Warn (don't fail) if something else already listens on the chosen port.
if command -v ss >/dev/null 2>&1 && ss -ltn "( sport = :${PORT} )" | grep -q LISTEN; then
  if ! $SUDO docker ps --format '{{.Names}}' | grep -q '^homelog-demo$'; then
    echo "⚠️  Port ${PORT} is already in use on this server. Set HOST_PORT in .env to another port."
  fi
fi

echo "==> Building and starting HomeLog demo (first build takes a few minutes)..."
$SUDO docker compose -f docker-compose.yml -f docker-compose.demo.yml up -d --build --remove-orphans

# Open the port in ufw when it is active.
if command -v ufw >/dev/null 2>&1 && $SUDO ufw status | grep -q "Status: active"; then
  $SUDO ufw allow "${PORT}/tcp" >/dev/null
fi

echo "==> Waiting for the app to become healthy..."
for _ in $(seq 1 60); do
  if curl -fsS "http://localhost:${PORT}/health" >/dev/null 2>&1; then
    IP=$(curl -fsS https://api.ipify.org 2>/dev/null || hostname -I | awk '{print $1}')
    echo "✅ HomeLog demo is up: http://${IP}$([ "${PORT}" = 80 ] || echo ":${PORT}")"
    exit 0
  fi
  sleep 3
done
echo "❌ The app did not become healthy. Recent logs:" >&2
$SUDO docker compose -f docker-compose.yml -f docker-compose.demo.yml logs --tail=50 >&2
exit 1
