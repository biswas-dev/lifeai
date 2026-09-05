#!/bin/bash
# Deploy lifeai to an environment by SSHing to the target server and building
# the image natively there — the same shape as taskai's fast deploy, with one
# container instead of five.
#
# Usage: bash deployment/scripts/deploy.sh <staging|uat|production>
#
# Expects, in the environment:
#   <ENV>_SSH_PRIVATE_KEY   the deploy key (raw PEM or base64)
#   <ENV>_JWT_SECRET, <ENV>_OAUTH_STATE_SECRET, <ENV>_ENCRYPTION_KEY
#   <ENV>_ADMIN_EMAIL, <ENV>_ADMIN_PASSWORD           (seeded on first boot)
#   <ENV>_GOOGLE_CLIENT_ID/SECRET, <ENV>_LOGIN_GITHUB_CLIENT_ID/SECRET (optional)
#   STRAVA_CLIENT_ID / STRAVA_CLIENT_SECRET            (optional, shared)
#   DEEPSEEK_API_KEY / NVIDIA_API_KEY                  (optional, shared)
#   HARD75_ALLOWED_EMAILS                              (optional)
#   CF_API_TOKEN / CF_ZONE_ID                          (optional, cache purge)

set -euo pipefail

ENV="${1:?Usage: deploy.sh <staging|uat|production>}"

case "$ENV" in
  staging)
    PREFIX="STAGING"; SERVER_IP="129.213.82.37"; DOMAIN="staging.lifeai.cc" ;;
  uat)
    PREFIX="UAT"; SERVER_IP="92.4.83.28"; DOMAIN="uat.lifeai.cc" ;;
  production)
    PREFIX="PRODUCTION"; SERVER_IP="31.97.102.48"; DOMAIN="lifeai.cc" ;;
  *)
    echo "ERROR: unknown environment: $ENV"; exit 1 ;;
esac

SERVER_USER="ubuntu"
DEPLOY_DIR="/home/ubuntu/lifeai-${ENV}"
PORT=13440
APP_URL="https://${DOMAIN}"

var() { local name="${PREFIX}_$1"; echo "${!name:-}"; }

SSH_KEY_VALUE="$(var SSH_PRIVATE_KEY)"
if [ -z "$SSH_KEY_VALUE" ]; then
  echo "ERROR: ${PREFIX}_SSH_PRIVATE_KEY is not set"; exit 1
fi
JWT_SECRET="$(var JWT_SECRET)"
if [ -z "$JWT_SECRET" ]; then
  echo "ERROR: ${PREFIX}_JWT_SECRET is not set"; exit 1
fi

VERSION=$(cat VERSION 2>/dev/null || echo "dev")
GIT_SHORT=$(git rev-parse --short HEAD)
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

echo "=== lifeai deploy: $ENV ($DOMAIN) version $VERSION ($GIT_SHORT) ==="

mkdir -p ~/.ssh
if echo "$SSH_KEY_VALUE" | head -1 | grep -q "BEGIN"; then
  printf '%s\n' "$SSH_KEY_VALUE" > ~/.ssh/lifeai_deploy_key
else
  echo "$SSH_KEY_VALUE" | base64 --decode > ~/.ssh/lifeai_deploy_key
fi
chmod 600 ~/.ssh/lifeai_deploy_key
if [ -z "${SSH_KNOWN_HOSTS:-}" ]; then
  echo "ERROR: SSH_KNOWN_HOSTS must contain the verified deploy host keys"; exit 1
fi
printf '%s\n' "$SSH_KNOWN_HOSTS" > ~/.ssh/lifeai_known_hosts
SSH="ssh -o StrictHostKeyChecking=yes -o UserKnownHostsFile=~/.ssh/lifeai_known_hosts -i ~/.ssh/lifeai_deploy_key $SERVER_USER@$SERVER_IP"
SCP="scp -o StrictHostKeyChecking=yes -o UserKnownHostsFile=~/.ssh/lifeai_known_hosts -i ~/.ssh/lifeai_deploy_key"
trap 'rm -f ~/.ssh/lifeai_deploy_key ~/.ssh/lifeai_known_hosts' EXIT

# --- 1. Sync a clean tarball of HEAD ---
echo "=== Syncing source ==="
$SSH "mkdir -p $DEPLOY_DIR/source $DEPLOY_DIR/data/photos"
TARBALL="/tmp/lifeai-deploy-$$.tar.gz"
git archive --format=tar HEAD | gzip > "$TARBALL"
$SCP "$TARBALL" "$SERVER_USER@$SERVER_IP:$TARBALL"
$SSH "cd $DEPLOY_DIR/source && tar xzf $TARBALL && rm -f $TARBALL"
rm -f "$TARBALL"

# --- 2. Write the environment file ---
echo "=== Writing environment ==="
ENV_FILE="$(mktemp)"
cat > "$ENV_FILE" <<ENVEOF
ENV=production
PORT=8080
APP_URL=${APP_URL}
LOG_LEVEL=info
DB_PATH=/data/lifeai.db
PHOTOS_DIR=/data/photos
FRONTEND_DIST=/app/web/dist
CORS_ALLOWED_ORIGINS=${APP_URL}
JWT_SECRET=${JWT_SECRET}
OAUTH_STATE_SECRET=$(var OAUTH_STATE_SECRET)
OAUTH_SUCCESS_URL=${APP_URL}/oauth/callback
OAUTH_ERROR_URL=${APP_URL}/login
GOOGLE_CLIENT_ID=$(var GOOGLE_CLIENT_ID)
GOOGLE_CLIENT_SECRET=$(var GOOGLE_CLIENT_SECRET)
LOGIN_GITHUB_CLIENT_ID=$(var LOGIN_GITHUB_CLIENT_ID)
LOGIN_GITHUB_CLIENT_SECRET=$(var LOGIN_GITHUB_CLIENT_SECRET)
ENCRYPTION_KEY=$(var ENCRYPTION_KEY)
ADMIN_EMAIL=$(var ADMIN_EMAIL)
ADMIN_PASSWORD=$(var ADMIN_PASSWORD)
HARD75_ALLOWED_EMAILS=${HARD75_ALLOWED_EMAILS:-anchoo2kewl@gmail.com}
HARD75_SYNC_HOURS=24
STRAVA_CLIENT_ID=${STRAVA_CLIENT_ID:-}
STRAVA_CLIENT_SECRET=${STRAVA_CLIENT_SECRET:-}
STRAVA_SYNC_MINUTES=30
AI_DAILY_LIMIT=60
ENVEOF
# DeepSeek leads: fast and cheap; NVIDIA is the backup and serves vision too.
if [ -n "${DEEPSEEK_API_KEY:-}" ]; then
cat >> "$ENV_FILE" <<ENVEOF
AI_1_PROVIDER=deepseek
AI_1_MODEL=deepseek-v4-flash
AI_1_API_KEY=${DEEPSEEK_API_KEY}
AI_1_TIMEOUT_SECONDS=60
AIV_1_PROVIDER=deepseek
AIV_1_MODEL=deepseek-v4-flash-vision-exp
AIV_1_API_KEY=${DEEPSEEK_API_KEY}
AIV_1_TIMEOUT_SECONDS=60
ENVEOF
fi
if [ -n "${NVIDIA_API_KEY:-}" ]; then
  if [ -n "${DEEPSEEK_API_KEY:-}" ]; then N=2; else N=1; fi
cat >> "$ENV_FILE" <<ENVEOF
AI_${N}_PROVIDER=nvidia
AI_${N}_MODEL=meta/llama-3.2-90b-vision-instruct
AI_${N}_API_KEY=${NVIDIA_API_KEY}
AI_${N}_TIMEOUT_SECONDS=60
AIV_${N}_PROVIDER=nvidia
AIV_${N}_MODEL=meta/llama-3.2-90b-vision-instruct
AIV_${N}_API_KEY=${NVIDIA_API_KEY}
AIV_${N}_TIMEOUT_SECONDS=60
ENVEOF
fi
$SCP "$ENV_FILE" "$SERVER_USER@$SERVER_IP:$DEPLOY_DIR/.env"
rm -f "$ENV_FILE"

# --- 3. nginx site (idempotent) ---
echo "=== nginx ==="
$SSH "sudo bash $DEPLOY_DIR/source/deployment/scripts/ensure-site.sh $DOMAIN $PORT"

# --- 4. Build and run on the server ---
echo "=== Building on server ==="
$SSH "bash -s" <<REMOTE_EOF
  set -e
  cd $DEPLOY_DIR
  export VERSION='$VERSION' GIT_COMMIT='$GIT_SHORT' BUILD_TIME='$BUILD_TIME' LIFEAI_PORT='$PORT'
  sed 's|context: \.|context: ./source|' source/docker-compose.yml > docker-compose.yml
  chmod 600 .env
  # The container runs as uid 1001; a bind mount takes the host directory's
  # ownership, so the data dir must be writable by that uid.
  sudo chown -R 1001:1001 data
  DISK_USAGE=\$(df / | tail -1 | awk '{print \$5}' | tr -d '%')
  if [ "\$DISK_USAGE" -gt 80 ]; then
    docker builder prune -f --filter "until=48h" || true
    docker image prune -f || true
  fi
  docker compose -p lifeai-$ENV build --build-arg VERSION='$VERSION' --build-arg GIT_COMMIT='$GIT_SHORT' --build-arg BUILD_TIME='$BUILD_TIME'
  docker compose -p lifeai-$ENV up -d --force-recreate --remove-orphans
  for i in \$(seq 1 40); do
    if curl -fsS http://127.0.0.1:$PORT/health > /dev/null; then echo "healthy after \${i}s"; exit 0; fi
    sleep 1
  done
  echo "::error::service did not become healthy"
  docker compose -p lifeai-$ENV logs --tail=100
  exit 1
REMOTE_EOF

# --- 5. Verify through the edge ---
echo "=== Verifying https://$DOMAIN/api/health ==="
sleep 3
if curl -fsS "https://$DOMAIN/api/health"; then
  echo; echo "Health check passed"
else
  echo "ERROR: edge health check failed"; exit 1
fi

if [ -n "${CF_API_TOKEN:-}" ] && [ -n "${CF_ZONE_ID:-}" ]; then
  curl -sS -X POST "https://api.cloudflare.com/client/v4/zones/${CF_ZONE_ID}/purge_cache" \
    -H "Authorization: Bearer ${CF_API_TOKEN}" -H "Content-Type: application/json" \
    -d "{\"hosts\":[\"${DOMAIN}\"]}" > /dev/null && echo "Cloudflare cache purged"
fi
echo "=== Deploy to $ENV complete ==="
