#!/bin/bash
# Installs (or refreshes) the nginx site for a lifeai domain and obtains a
# Let's Encrypt certificate if there is none. Idempotent; run as root on the
# target server.
#
# Usage: ensure-site.sh <domain> <loopback-port>
set -euo pipefail
DOMAIN="${1:?domain}"
PORT="${2:?port}"
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE="$HERE/../nginx-site.conf.template"
SITE="/etc/nginx/sites-enabled/$DOMAIN"
ZONE="$(echo "$DOMAIN" | tr '.-' '__')"

render() {
  sed -e "s/\${DOMAIN}/$DOMAIN/g" -e "s/\${PORT}/$PORT/g" -e "s/\${ZONE}/$ZONE/g" "$TEMPLATE"
}

mkdir -p /var/www/html

if [ ! -f "/etc/letsencrypt/live/$DOMAIN/fullchain.pem" ]; then
  echo "No certificate for $DOMAIN yet; installing the HTTP-only site to answer the ACME challenge"
  cat > "$SITE" <<HTTP
server {
    listen 80;
    listen [::]:80;
    server_name $DOMAIN;
    location /.well-known/acme-challenge/ { root /var/www/html; }
    location / { proxy_pass http://127.0.0.1:$PORT; proxy_set_header Host \$host; }
}
HTTP
  nginx -t
  nginx -s reload
  certbot certonly --webroot -w /var/www/html -d "$DOMAIN" --non-interactive --agree-tos \
    --email anshuman@biswas.me --keep-until-expiring || {
      echo "certbot failed; leaving the HTTP-only site in place"; exit 0; }
fi

NEW="$(mktemp)"
render > "$NEW"
if [ -f "$SITE" ] && cmp -s "$NEW" "$SITE"; then
  rm -f "$NEW"
  echo "nginx site for $DOMAIN unchanged"
  exit 0
fi
PREV="$(mktemp)"
[ -f "$SITE" ] && cp "$SITE" "$PREV"
cp "$NEW" "$SITE"
rm -f "$NEW"
if ! nginx -t; then
  echo "nginx rejected the new site; restoring the previous one"
  if [ -s "$PREV" ]; then cp "$PREV" "$SITE"; else rm -f "$SITE"; fi
  rm -f "$PREV"
  exit 1
fi
rm -f "$PREV"
nginx -s reload
echo "nginx site for $DOMAIN installed"
