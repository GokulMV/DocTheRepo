#!/usr/bin/env bash
# DocTheRepo Hub quickstart: one command from a fresh machine to the web UI.
#
#   ./scripts/quickstart.sh                 # check/install Docker, build, start, sign in, open the UI
#   ANTHROPIC_API_KEY=... OPENAI_API_KEY=... ./scripts/quickstart.sh   # also configure models
#   ./scripts/quickstart.sh --down          # stop (add --wipe to delete data)
#
# It checks every prerequisite and installs what is missing, unattended:
#   git, curl, tar, make, openssl, a C compiler (the Tree-sitter grammars are C, so the hub builds with cgo),
#   Go 1.25.13 and Node.js 22.12+ (into ~/.dth-quickstart/toolchain when the system ones are missing or too old),
#   and Docker + Compose v2 for PostgreSQL/pgvector (Linux: get.docker.com; macOS: Homebrew + Colima).
# Then it builds the UI and the hub from this checkout (make release), starts PostgreSQL in Docker, runs the
# hub, signs in, and opens the UI. Re-running is safe: passwords are reused and the hub is rebuilt/restarted.
# --container skips the host Go/Node toolchain and builds the container image instead.
#
# Options (or environment variables):
#   --port N          DTH_PORT            host port (default 8080)
#   --email ADDR      DTH_OWNER_EMAIL     owner account (default admin@dth.local)
#   --container                           run the hub as a container image built by Docker (no host Go/Node)
#   --image REF       DTH_IMAGE           with --container: use a published image instead of building one
#   --no-browser      DTH_NO_BROWSER=1    do not open a browser
#   --settings PATH   DTH_SETTINGS_FILE   settings file or directory applied when the hub starts (sign-in, SSO,
#                                         users, models, repositories); export the secrets it references first
#   --init                                ask the setup questions first (dth init) and use the file it writes
#   --down [--wipe]                       stop the stack (and delete its volumes)
# Model keys (optional, configured through the API so the setup wizard has nothing left to ask):
#   ANTHROPIC_API_KEY   chat features (docgen, qa, decode, triage, suggest)
#   OPENAI_API_KEY      embeddings (text-embedding-3-small); chat too if DTH_OPENAI_CHAT_MODEL is set
#   DTH_ANTHROPIC_MODEL / DTH_ANTHROPIC_FAST_MODEL override the default Claude models
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STATE_DIR="${DTH_QUICKSTART_DIR:-$HOME/.dth-quickstart}"
PROJECT=dth-quickstart
PORT="${DTH_PORT:-8080}"
EMAIL="${DTH_OWNER_EMAIL:-admin@dth.local}"
IMAGE="${DTH_IMAGE:-}"
NO_BROWSER="${DTH_NO_BROWSER:-}"
ACTION=up
WIPE=
MODE="${DTH_QUICKSTART_MODE:-native}"
GO_VERSION=1.25.13
NODE_VERSION=22.12.0
PG_PORT="${DTH_PG_PORT:-54329}"
SETTINGS="${DTH_SETTINGS_FILE:-}"
INIT=

while [ $# -gt 0 ]; do
  case "$1" in
    --port) PORT="$2"; shift 2 ;;
    --email) EMAIL="$2"; shift 2 ;;
    --image) IMAGE="$2"; MODE=container; shift 2 ;;
    --container) MODE=container; shift ;;
    --no-browser) NO_BROWSER=1; shift ;;
    --settings) SETTINGS="$2"; shift 2 ;;
    --init) INIT=1; shift ;;
    --down) ACTION=down; shift ;;
    --wipe) WIPE=1; shift ;;
    -h|--help) sed -n '2,31p' "$0"; exit 0 ;;
    *) echo "unknown option: $1 (see --help)" >&2; exit 2 ;;
  esac
done

say()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31mxx\033[0m %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

OS="$(uname -s)"
SUDO=
if [ "$(id -u)" -ne 0 ]; then SUDO=sudo; fi
DOCKER=(docker)

# ---------------------------------------------------------------------------------------------------------
# Prerequisites
# ---------------------------------------------------------------------------------------------------------
pkg_install() { # generic-name... → install with the system package manager
  if [ "$OS" = Darwin ]; then
    ensure_brew; brew install "$@" >/dev/null
  elif have apt-get; then
    $SUDO env DEBIAN_FRONTEND=noninteractive apt-get update -qq
    $SUDO env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq "$@"
  elif have dnf; then $SUDO dnf install -y -q "$@"
  elif have yum; then $SUDO yum install -y -q "$@"
  elif have apk; then $SUDO apk add --no-cache "$@"
  elif have zypper; then $SUDO zypper -n install "$@"
  elif have pacman; then $SUDO pacman -S --noconfirm --needed "$@"
  else die "no supported package manager: please install $*"
  fi
}

need_basic_tools() {
  local missing=()
  for t in git curl tar openssl; do have "$t" || missing+=("$t"); done
  if [ "$MODE" = native ] && ! have make; then missing+=(make); fi
  if [ ${#missing[@]} -gt 0 ]; then
    say "Installing ${missing[*]}"
    pkg_install "${missing[@]}"
  fi
  [ "$MODE" = native ] && ensure_cc
  return 0
}

# ensure_cc installs a C compiler (cgo builds the built-in Tree-sitter grammars).
ensure_cc() {
  if have cc || have gcc || have clang; then
    if [ "$OS" != Darwin ] || xcode-select -p >/dev/null 2>&1; then return; fi
  fi
  say "Installing a C compiler"
  if [ "$OS" = Darwin ]; then
    # Unattended Command Line Tools install (no GUI dialog).
    touch /tmp/.com.apple.dt.CommandLineTools.installondemand.in-progress
    local label
    label="$(softwareupdate -l 2>/dev/null | sed -n 's/^.*Label: \(Command Line Tools.*\)$/\1/p' | sort -V | tail -1)"
    [ -n "$label" ] || die "could not find the Command Line Tools update: run xcode-select --install"
    $SUDO softwareupdate -i "$label" --verbose >/dev/null
    rm -f /tmp/.com.apple.dt.CommandLineTools.installondemand.in-progress
  elif have apt-get; then pkg_install build-essential
  elif have apk; then pkg_install build-base
  elif have pacman; then pkg_install base-devel
  else pkg_install gcc gcc-c++ make
  fi
  have cc || have gcc || have clang || die "C compiler installation failed"
}

arch_name() { # → amd64 | arm64
  case "$(uname -m)" in
    x86_64|amd64) echo amd64 ;;
    arm64|aarch64) echo arm64 ;;
    *) die "unsupported CPU $(uname -m)" ;;
  esac
}

# ensure_go puts Go $GO_VERSION on PATH: the system go if it is that version, else a private download.
ensure_go() {
  local tc="$STATE_DIR/toolchain/go$GO_VERSION"
  # GOTOOLCHAIN=local: an older go would otherwise auto-switch and report the wanted version.
  if have go && [ "$(GOTOOLCHAIN=local go env GOVERSION 2>/dev/null)" = "go$GO_VERSION" ]; then :
  elif [ -x "$tc/bin/go" ]; then export PATH="$tc/bin:$PATH"
  else
    local os plat tmp; os="$(echo "$OS" | tr '[:upper:]' '[:lower:]')"; plat="$os-$(arch_name)"
    say "Installing Go $GO_VERSION into $tc"
    tmp="$(mktemp -d)"
    # go.dev first; the Go module proxy (what GOTOOLCHAIN uses) when go.dev is unreachable.
    if curl -fsSL -o "$tmp/go.tgz" "https://go.dev/dl/go$GO_VERSION.$plat.tar.gz"; then
      mkdir -p "$tmp/go" && tar -xzf "$tmp/go.tgz" -C "$tmp/go" --strip-components=1
    else
      warn "go.dev unreachable; using proxy.golang.org"
      have unzip || pkg_install unzip
      curl -fsSL -o "$tmp/go.zip" "https://proxy.golang.org/golang.org/toolchain/@v/v0.0.1-go$GO_VERSION.$plat.zip" ||
        die "could not download Go $GO_VERSION (go.dev and proxy.golang.org both failed)"
      unzip -q "$tmp/go.zip" -d "$tmp/zip"
      mv "$tmp/zip/golang.org/toolchain@v0.0.1-go$GO_VERSION.$plat" "$tmp/go"
      chmod -R u+x "$tmp/go/bin" "$tmp/go/pkg/tool" 2>/dev/null || true
    fi
    rm -rf "$tc"; mkdir -p "$(dirname "$tc")"; mv "$tmp/go" "$tc"; rm -rf "$tmp"
    export PATH="$tc/bin:$PATH"
  fi
  export GOTOOLCHAIN=local CGO_ENABLED=1
  say "Go $(go env GOVERSION) is ready"
}

# node_ok: the UI build (Vite 8) needs Node.js ^20.19 or >=22.12.
node_ok() {
  have node && have npm || return 1
  local v major minor
  v="$(node -p 'process.versions.node' 2>/dev/null)" || return 1
  major="${v%%.*}"; minor="$(echo "$v" | cut -d. -f2)"
  [ "$major" -gt 22 ] || { [ "$major" -eq 22 ] && [ "$minor" -ge 12 ]; } || { [ "$major" -eq 20 ] && [ "$minor" -ge 19 ]; }
}

# ensure_node puts a suitable Node.js (with npm) on PATH: the system one or a private download.
ensure_node() {
  local tc="$STATE_DIR/toolchain/node-v$NODE_VERSION"
  if node_ok; then :
  elif [ -x "$tc/bin/node" ]; then export PATH="$tc/bin:$PATH"
  else
    local os arch
    os="$(echo "$OS" | tr '[:upper:]' '[:lower:]')"
    arch="$(arch_name)"; [ "$arch" = amd64 ] && arch=x64
    say "Installing Node.js $NODE_VERSION into $tc"
    local tmp; tmp="$(mktemp -d)"
    curl -fsSL -o "$tmp/node.tgz" "https://nodejs.org/dist/v$NODE_VERSION/node-v$NODE_VERSION-$os-$arch.tar.gz" ||
      die "could not download Node.js $NODE_VERSION from nodejs.org"
    mkdir -p "$tmp/node" && tar -xzf "$tmp/node.tgz" -C "$tmp/node" --strip-components=1
    rm -rf "$tc"; mkdir -p "$(dirname "$tc")"; mv "$tmp/node" "$tc"; rm -rf "$tmp"
    export PATH="$tc/bin:$PATH"
  fi
  say "Node.js $(node --version) / npm $(npm --version) are ready"
}

ensure_brew() {
  have brew && return
  say "Installing Homebrew (unattended)"
  NONINTERACTIVE=1 /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
  for b in /opt/homebrew/bin/brew /usr/local/bin/brew; do [ -x "$b" ] && eval "$("$b" shellenv)"; done
  have brew || die "Homebrew installation failed"
}

docker_ok() { "${DOCKER[@]}" info >/dev/null 2>&1; }

wait_docker() {
  for _ in $(seq 1 90); do docker_ok && return 0; sleep 2; done
  return 1
}

ensure_docker_linux() {
  if ! have docker; then
    say "Installing Docker Engine (get.docker.com)"
    curl -fsSL https://get.docker.com | $SUDO sh
  fi
  if ! docker_ok; then
    if have systemctl; then $SUDO systemctl enable --now docker >/dev/null 2>&1 || true
    elif have service; then $SUDO service docker start >/dev/null 2>&1 || true
    fi
  fi
  # A user just added to the docker group only gets it in a new login session: use sudo until then.
  if ! docker_ok && [ -n "$SUDO" ] && $SUDO docker info >/dev/null 2>&1; then
    DOCKER=($SUDO docker)
    $SUDO usermod -aG docker "$USER" 2>/dev/null || true
  fi
  wait_docker || die "Docker is installed but the daemon is not reachable"
  if ! "${DOCKER[@]}" compose version >/dev/null 2>&1; then
    say "Installing the Docker Compose plugin"
    if have apt-get; then $SUDO env DEBIAN_FRONTEND=noninteractive apt-get install -y -qq docker-compose-plugin docker-buildx-plugin
    elif have dnf; then $SUDO dnf install -y -q docker-compose-plugin docker-buildx-plugin
    else die "install the Docker Compose v2 plugin"
    fi
  fi
}

ensure_docker_mac() {
  if docker_ok; then return; fi
  if [ -d /Applications/Docker.app ]; then
    say "Starting Docker Desktop"
    open -ga Docker
    wait_docker && return
  fi
  ensure_brew
  say "Installing Colima + Docker CLI (no GUI, no license prompt)"
  brew install colima docker docker-compose docker-buildx >/dev/null
  mkdir -p "$HOME/.docker/cli-plugins"
  ln -sfn "$(brew --prefix)/opt/docker-compose/bin/docker-compose" "$HOME/.docker/cli-plugins/docker-compose"
  ln -sfn "$(brew --prefix)/opt/docker-buildx/bin/docker-buildx" "$HOME/.docker/cli-plugins/docker-buildx"
  colima status >/dev/null 2>&1 || colima start --cpu 4 --memory 6 --disk 40
  wait_docker || die "Colima started but Docker is not reachable"
}

ensure_docker() {
  case "$OS" in
    Linux) ensure_docker_linux ;;
    Darwin) ensure_docker_mac ;;
    *) die "unsupported OS $OS (on Windows, run this inside WSL 2)" ;;
  esac
  "${DOCKER[@]}" compose version >/dev/null 2>&1 || die "Docker Compose v2 is required"
  say "Docker $("${DOCKER[@]}" version --format '{{.Server.Version}}' 2>/dev/null) is ready"
}

compose() {
  "${DOCKER[@]}" compose -p "$PROJECT" --env-file "$STATE_DIR/.env" -f "$STATE_DIR/compose.yaml" "$@"
}

# The data volume outlives the state directory: if .env was deleted, Postgres still has the old password and
# the hub cannot connect. Inside the container the local socket is trusted, so re-apply the current password.
sync_db_password() {
  compose exec -T postgres psql -q -U dth -d dth -v ON_ERROR_STOP=1 \
    -c "ALTER USER dth WITH PASSWORD '$DB_PW'" >/dev/null || die "could not set the database password"
}

# ---------------------------------------------------------------------------------------------------------
# Down
# ---------------------------------------------------------------------------------------------------------
PID_FILE="$STATE_DIR/hub.pid"
LOG_FILE="$STATE_DIR/hub.log"

stop_native_hub() {
  [ -f "$PID_FILE" ] || return 0
  local pid; pid="$(cat "$PID_FILE")"
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    for _ in $(seq 1 30); do kill -0 "$pid" 2>/dev/null || break; sleep 1; done
    kill -9 "$pid" 2>/dev/null || true
  fi
  rm -f "$PID_FILE"
}

if [ "$ACTION" = down ]; then
  stop_native_hub
  [ -f "$STATE_DIR/compose.yaml" ] || { say "Nothing to stop"; exit 0; }
  ensure_docker
  if [ -n "$WIPE" ]; then compose down -v; rm -rf "$STATE_DIR"; say "Stopped and wiped"
  else compose down; say "Stopped (data kept; --down --wipe deletes it)"
  fi
  exit 0
fi

# ---------------------------------------------------------------------------------------------------------
# Up
# ---------------------------------------------------------------------------------------------------------
mkdir -p "$STATE_DIR"
chmod 700 "$STATE_DIR"
say "Checking prerequisites ($MODE mode)"
need_basic_tools
if [ "$MODE" = native ]; then
  ensure_go
  ensure_node
fi
ensure_docker

ENV_FILE="$STATE_DIR/.env"
# A database volume without its .env means the owner password and master key that go with it are gone.
if [ ! -f "$ENV_FILE" ] && "${DOCKER[@]}" volume inspect "${PROJECT}_pgdata" >/dev/null 2>&1; then
  die "found a database from an earlier quickstart, but its credentials ($ENV_FILE) are gone: run '$0 --down --wipe' to start fresh"
fi
# Native mode keeps the master key (which encrypts every stored API key and token) next to .env. Without it
# the database's secrets cannot be read, so refuse rather than start with connectors that silently fail.
if [ "$MODE" = native ] && [ -f "$ENV_FILE" ] && [ ! -f "$STATE_DIR/master.key" ] && "${DOCKER[@]}" volume inspect "${PROJECT}_pgdata" >/dev/null 2>&1; then
  die "the master key ($STATE_DIR/master.key) that encrypts your stored keys is missing: restore it from a backup, or run '$0 --down --wipe' to start fresh"
fi
get_env() { if [ -f "$ENV_FILE" ]; then sed -n "s/^$1=//p" "$ENV_FILE" | tail -1; fi; }
DB_PW="$(get_env DTH_DB_PASSWORD)"; [ -n "$DB_PW" ] || DB_PW="$(openssl rand -hex 24)"
OWNER_PW="$(get_env DTH_OWNER_PASSWORD)"; [ -n "$OWNER_PW" ] || OWNER_PW="$(openssl rand -base64 18 | tr -d '/+=' | cut -c1-20)"
PREV_EMAIL="$(get_env DTH_OWNER_EMAIL)"; if [ -n "$PREV_EMAIL" ]; then EMAIL="$PREV_EMAIL"; fi
# Settings applied at start live in $STATE_DIR/settings (Compose mounts it in container mode); a re-run
# without --settings keeps the ones copied there before.
mkdir -p "$STATE_DIR/settings"
if [ -n "$SETTINGS" ]; then
  [ -e "$SETTINGS" ] || die "settings file not found: $SETTINGS"
  rm -f "$STATE_DIR/settings/"*.yaml "$STATE_DIR/settings/"*.yml "$STATE_DIR/settings/"*.json
  if [ -d "$SETTINGS" ]; then cp "$SETTINGS"/*.y*ml "$SETTINGS"/*.json "$STATE_DIR/settings/" 2>/dev/null || true
  else cp "$SETTINGS" "$STATE_DIR/settings/"
  fi
  say "Settings from $SETTINGS are applied when the hub starts"
fi
URL="http://localhost:$PORT"

port_busy() { (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }

if [ "$MODE" = native ]; then
  stop_native_hub
  say "Building the web UI and the hub from $REPO_ROOT (make release)"
  make -C "$REPO_ROOT" release GO=go
  if [ -n "$INIT" ]; then
    "$REPO_ROOT/bin/dth" init -o "$STATE_DIR/settings/hub.yaml" --force </dev/tty
    say "Export the secrets listed above, then this run continues in 5 seconds (Ctrl-C to stop and export them first)"
    sleep 5
  fi
  cat > "$STATE_DIR/compose.yaml" <<'EOF'
# PostgreSQL (pgvector) for the quickstart hub, which runs natively from this checkout.
name: dth-quickstart
services:
  postgres:
    image: pgvector/pgvector:pg16
    restart: unless-stopped
    environment:
      POSTGRES_USER: dth
      POSTGRES_PASSWORD: ${DTH_DB_PASSWORD:?missing}
      POSTGRES_DB: dth
    ports:
      - "127.0.0.1:${DTH_PG_PORT:-54329}:5432"
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U dth -d dth"]
      interval: 2s
      timeout: 3s
      retries: 60
volumes:
  pgdata:
EOF
else
  if [ -z "$IMAGE" ]; then
    IMAGE=doctherepo-hub:local
    say "Building the hub image (UI + hub + CLI; first build takes a few minutes)"
    DOCKER_BUILDKIT=1 "${DOCKER[@]}" build -f "$REPO_ROOT/docker/Dockerfile" \
      --build-arg VERSION="$(git -C "$REPO_ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)" \
      -t "$IMAGE" "$REPO_ROOT"
  else
    say "Pulling $IMAGE"
    "${DOCKER[@]}" pull "$IMAGE"
  fi
  cp "$REPO_ROOT/internal/bootstrap/compose.yaml" "$STATE_DIR/compose.yaml"
  if [ -n "$INIT" ]; then
    "${DOCKER[@]}" run --rm -it --entrypoint /dth -v "$STATE_DIR/settings:/out" -w /out "$IMAGE" init -o /out/hub.yaml --force
    say "Export the secrets listed above, then this run continues in 5 seconds (Ctrl-C to stop and export them first)"
    sleep 5
  fi
fi

# The hub container runs as a non-root user: let it read the settings (they hold references, not secrets).
chmod a+rx "$STATE_DIR/settings"; chmod a+r "$STATE_DIR/settings/"* 2>/dev/null || true
umask 077
cat > "$ENV_FILE" <<EOF
DTH_DB_PASSWORD=$DB_PW
DTH_OWNER_EMAIL=$EMAIL
DTH_OWNER_PASSWORD=$OWNER_PW
DTH_IMAGE=${IMAGE:-}
DTH_PORT=$PORT
DTH_PG_PORT=$PG_PORT
DTH_PUBLIC_URL=$URL
DTH_AUTH_MODE=local
EOF

if port_busy "$PORT" && ! curl -fsS "$URL/readyz" >/dev/null 2>&1; then
  die "port $PORT is in use by something else: re-run with --port <free port>"
fi

if [ "$MODE" = native ]; then
  say "Starting PostgreSQL (pgvector) on 127.0.0.1:$PG_PORT"
  compose up -d --remove-orphans --wait
  sync_db_password
  if port_busy "$PORT"; then compose down >/dev/null 2>&1 || true; die "port $PORT is in use: re-run with --port <free port>"; fi
  say "Starting the hub (log: $LOG_FILE)"
  (
    cd "$STATE_DIR"
    export DTH_DATABASE_URL="postgres://dth:$DB_PW@127.0.0.1:$PG_PORT/dth?sslmode=disable"
    export DTH_LISTEN="127.0.0.1:$PORT" DTH_PUBLIC_URL="$URL" DTH_AUTH_MODE=local
    export DTH_OWNER_EMAIL="$EMAIL" DTH_OWNER_PASSWORD="$OWNER_PW"
    export DTH_LOCAL_KEY_FILE="$STATE_DIR/master.key" DTH_GRAMMARS_DIR="$STATE_DIR/grammars" DTH_LOG_FORMAT=text
    export DTH_SETTINGS_FILE="$STATE_DIR/settings"
    mkdir -p "$STATE_DIR/grammars"
    nohup "$REPO_ROOT/bin/dth-hub" >>"$LOG_FILE" 2>&1 &
    echo $! > "$PID_FILE"
  )
else
  say "Starting PostgreSQL (pgvector) and the hub container"
  compose up -d --remove-orphans --wait postgres
  sync_db_password
  compose up -d --remove-orphans
fi

say "Waiting for $URL/readyz"
ready=
for _ in $(seq 1 120); do
  if curl -fsS "$URL/readyz" >/dev/null 2>&1; then ready=1; break; fi
  if [ "$MODE" = native ] && ! kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then break; fi
  sleep 2
done
if [ -z "$ready" ]; then
  if [ "$MODE" = native ]; then tail -n 80 "$LOG_FILE" >&2; else compose logs --tail 80 hub >&2; fi
  die "the hub did not become ready (logs above)"
fi

# ---------------------------------------------------------------------------------------------------------
# Optional model configuration through the API (as the owner)
# ---------------------------------------------------------------------------------------------------------
JAR="$STATE_DIR/cookies"
api_login() {
  local body
  body="$(curl -fsS -c "$JAR" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$EMAIL\",\"password\":\"$OWNER_PW\"}" "$URL/api/v1/auth/local/login")" || return 1
  CSRF="$(printf '%s' "$body" | sed -n 's/.*"csrf_token":"\([^"]*\)".*/\1/p')"
  [ -n "$CSRF" ]
}
api() { # method path [json]
  curl -fsS -b "$JAR" -X "$1" -H "X-CSRF-Token: $CSRF" -H 'Content-Type: application/json' ${3:+-d "$3"} "$URL/api/v1$2"
}
provider_id() { # kind → existing provider id
  api GET /providers | tr '{' '\n' | grep "\"kind\":\"$1\"" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p' | head -1
}
ensure_provider() { # kind name key → id
  local id
  id="$(provider_id "$1")"
  if [ -z "$id" ]; then
    id="$(api POST /providers "{\"kind\":\"$1\",\"name\":\"$2\",\"api_key\":\"$3\"}" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p' | head -1)"
  fi
  printf '%s' "$id"
}
ROUTED=""
routed() { # feature → true if it already has a route (set in the UI or by an earlier run)
  [ -n "$ROUTED" ] || ROUTED="$(api GET /routes | tr '{' '\n' | sed -n 's/.*"feature":"\([^"]*\)".*/\1/p' | tr '\n' ' ')"
  case " $ROUTED " in *" $1 "*) return 0 ;; esac
  return 1
}
# route sets a feature's model only if it has none: re-running never overwrites routing changed in the UI.
route() {
  if routed "$1"; then echo "    route $1: kept as configured"; return 0; fi
  api PUT "/routes/$1" "{\"provider_id\":\"$2\",\"model\":\"$3\"}" >/dev/null && echo "    route $1 → $3"
}

if [ -n "${ANTHROPIC_API_KEY:-}${OPENAI_API_KEY:-}" ]; then
  if api_login; then
    say "Configuring model providers from your environment"
    if [ -n "${ANTHROPIC_API_KEY:-}" ]; then
      pid="$(ensure_provider anthropic Anthropic "$ANTHROPIC_API_KEY")"
      if [ -n "$pid" ]; then
        main="${DTH_ANTHROPIC_MODEL:-claude-sonnet-5-5}"; fast="${DTH_ANTHROPIC_FAST_MODEL:-claude-haiku-4-5-20251001}"
        for f in docgen qa decode suggest; do route "$f" "$pid" "$main" || warn "could not set route $f"; done
        route triage "$pid" "$fast" || warn "could not set route triage"
      else warn "could not create the Anthropic provider"
      fi
    fi
    if [ -n "${OPENAI_API_KEY:-}" ]; then
      pid="$(ensure_provider openai OpenAI "$OPENAI_API_KEY")"
      if [ -n "$pid" ]; then
        route embedding "$pid" "${DTH_OPENAI_EMBED_MODEL:-text-embedding-3-small}" || warn "could not set route embedding"
        if [ -n "${DTH_OPENAI_CHAT_MODEL:-}" ] && [ -z "${ANTHROPIC_API_KEY:-}" ]; then
          for f in docgen qa decode suggest triage; do route "$f" "$pid" "$DTH_OPENAI_CHAT_MODEL" || true; done
        fi
      else warn "could not create the OpenAI provider"
      fi
    fi
  else
    warn "could not sign in through the API; configure models in the UI under Providers & routing"
  fi
fi
rm -f "$JAR"

# ---------------------------------------------------------------------------------------------------------
# Done
# ---------------------------------------------------------------------------------------------------------
CREDS="$STATE_DIR/credentials.txt"
printf 'URL: %s\nEmail: %s\nPassword: %s\n' "$URL" "$EMAIL" "$OWNER_PW" > "$CREDS"
cat <<EOF

  DocTheRepo Hub is running at $URL

  Sign in:  $EMAIL
            $OWNER_PW
  (saved in $CREDS)

  Stop:     $0 --down        Wipe: $0 --down --wipe
  Logs:     $( [ "$MODE" = native ] && echo "tail -f $LOG_FILE" || echo "docker compose -p $PROJECT logs -f hub" )

EOF

if [ -z "$NO_BROWSER" ]; then
  if [ "$OS" = Darwin ]; then open "$URL" >/dev/null 2>&1 || true
  elif have xdg-open && [ -n "${DISPLAY:-}${WAYLAND_DISPLAY:-}" ]; then xdg-open "$URL" >/dev/null 2>&1 || true
  elif grep -qi microsoft /proc/version 2>/dev/null && have cmd.exe; then cmd.exe /c start "$URL" >/dev/null 2>&1 || true
  else echo "  (no desktop browser detected: open $URL yourself)"
  fi
fi
