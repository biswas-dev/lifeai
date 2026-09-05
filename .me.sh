#!/usr/bin/env bash
# lifeai adapter for the shared ./scripts/server runner (github.com/anchoo2kewl/me).

PROJECT_NAME="lifeai"
PROJECT_DOMAIN="lifeai.cc"
PROJECT_REPO="biswas-dev/lifeai"
PROJECT_STACK="Go + React + SQLite"
PROJECT_PORT_BACKEND=8088
PROJECT_PORT_FRONTEND=5176
PROJECT_DB="sqlite"

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
API_DIR="$PROJECT_ROOT/api"
WEB_DIR="$PROJECT_ROOT/web"
DATA_DIR="$PROJECT_ROOT/data"

_deploy_path() {
    case "$1" in
        staging) echo "/home/ubuntu/lifeai-staging" ;;
        uat)     echo "/home/ubuntu/lifeai-uat" ;;
        prod)    echo "/home/ubuntu/lifeai-production" ;;
    esac
}
_project() {
    case "$1" in
        staging) echo "lifeai-staging" ;;
        uat)     echo "lifeai-uat" ;;
        prod)    echo "lifeai-production" ;;
    esac
}

local_start() {
    print_header "Starting lifeai"
    mkdir -p "$DATA_DIR/photos"
    port_alive "$PROJECT_PORT_BACKEND" && kill_port "$PROJECT_PORT_BACKEND"
    (cd "$WEB_DIR" && npm run build) || { print_error "web build failed"; return 1; }
    (
        cd "$API_DIR" || exit 1
        ENV=development PORT="$PROJECT_PORT_BACKEND" DB_PATH="$DATA_DIR/lifeai.db" PHOTOS_DIR="$DATA_DIR/photos" \
        FRONTEND_DIST="$WEB_DIR/dist" JWT_SECRET="${JWT_SECRET:-dev-secret-change-me}" ENCRYPTION_KEY="${ENCRYPTION_KEY:-dev-encryption-key}" \
        go run ./cmd/api
    ) > "$PROJECT_ROOT/api.log" 2>&1 &
    sleep 3
    port_status "$PROJECT_PORT_BACKEND" "API"
    print_success "http://localhost:$PROJECT_PORT_BACKEND"
}

local_dev() {
    print_header "lifeai dev"
    mkdir -p "$DATA_DIR/photos"
    (
        cd "$API_DIR" || exit 1
        ENV=development PORT="$PROJECT_PORT_BACKEND" DB_PATH="$DATA_DIR/lifeai.db" PHOTOS_DIR="$DATA_DIR/photos" \
        JWT_SECRET="${JWT_SECRET:-dev-secret-change-me}" ENCRYPTION_KEY="${ENCRYPTION_KEY:-dev-encryption-key}" go run ./cmd/api
    ) > "$PROJECT_ROOT/api.log" 2>&1 &
    sleep 2
    (cd "$WEB_DIR" && npm run dev)
}

local_stop()    { kill_port "$PROJECT_PORT_BACKEND"; kill_port "$PROJECT_PORT_FRONTEND"; print_success "Stopped"; }
local_restart() { local_stop; sleep 1; local_start; }
local_status()  { port_status "$PROJECT_PORT_BACKEND" "API"; port_status "$PROJECT_PORT_FRONTEND" "Vite"; }
local_logs()    { tail -f "$PROJECT_ROOT/api.log"; }
local_test()    { local_test_backend && local_test_frontend; }
local_test_backend()  { (cd "$API_DIR" && go vet ./... && go test ./...); }
local_test_frontend() { (cd "$WEB_DIR" && npx tsc --noEmit && npx vitest run && npx vite build); }
local_db_migrate() { print_status "Migrations run at boot"; }
local_db_reset() {
    read -r -p "Type 'reset' to delete $DATA_DIR: " reply
    [ "$reply" = "reset" ] || return 0
    rm -rf "${DATA_DIR:?}"; mkdir -p "$DATA_DIR/photos"; print_success "Reset"
}
local_users() { sqlite3 -header -column "$DATA_DIR/lifeai.db" "SELECT id, email, name, is_admin, created_at FROM users WHERE deleted_at IS NULL;"; }

remote_status()  { ssh_cmd "$(resolve_server "$1")" "cd $(_deploy_path "$1") && docker compose -p $(_project "$1") ps"; }
remote_logs()    { ssh_cmd "$(resolve_server "$1")" "cd $(_deploy_path "$1") && docker compose -p $(_project "$1") logs --tail=200 -f"; }
remote_health()  { ssh_cmd "$(resolve_server "$1")" "curl -fsS http://127.0.0.1:13440/health && echo"; }
remote_restart() { ssh_cmd "$(resolve_server "$1")" "cd $(_deploy_path "$1") && docker compose -p $(_project "$1") restart"; }
remote_users()   { ssh_cmd "$(resolve_server "$1")" "cd $(_deploy_path "$1") && docker compose -p $(_project "$1") exec lifeai /app/admin users"; }
