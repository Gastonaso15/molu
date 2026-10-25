#!/usr/bin/env bash
# Levanta un xolu local con datos de demo de Seam para probar molu.
#
#   ./scripts/levantar-xolu.sh                 arranca xolu (puerto 9191) y carga la semilla 01 si la base está vacía
#   ./scripts/levantar-xolu.sh --seed 05       usa otra semilla (01 a 06)
#   ./scripts/levantar-xolu.sh --reset         borra los datos y vuelve a sembrar
#   ./scripts/levantar-xolu.sh --no-seed       arranca sin cargar semilla
#   ./scripts/levantar-xolu.sh --port 9292     otro puerto
#   ./scripts/levantar-xolu.sh stop            apaga el xolu que lanzó este script
#   ./scripts/levantar-xolu.sh status          dice si está corriendo
#
# Todo el estado (binario, datos, entorno de Python, log) vive en .dev-xolu/
# en la raíz del repo (ignorado por git) y se puede borrar sin riesgo.
#
# Hace falta el código de xolu (para compilarlo) y las semillas de Seam, que
# no viven en este repo. Si están en ../SEAM-CURE/ junto al repo se encuentran
# solos; si no, indica dónde:
#   XOLU_SRC=/ruta/a/xolu          carpeta con cmd/xolu (se compila con Go)
#   XOLU_BIN=/ruta/a/xolu          o un binario ya compilado (no hace falta Go)
#   SEEDS_DIR=/ruta/a/seam/seeds   carpeta con seed-01-*.py ... seed-06-*.py
#
# xolu se arranca con /api/v2 y calendario activados (XOLU_API_V2_ENABLED y
# XOLU_CAL_ENABLED): sin ellos, walk, machine_history y cal_* dan 404.
# La semilla 01 no registra esquemas en /api/v1/schemas; molu usa sus 13
# primitivos fijos.

set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STATE="${STATE:-$REPO/.dev-xolu}"

# Carpetas hermanas del repo donde suelen estar las versiones descargadas.
latest() { ls -d "$@" 2>/dev/null | sort -V | tail -1 || true; }
XOLU_SRC="${XOLU_SRC:-$(latest "$REPO"/../SEAM-CURE/xolu-v*/)}"
SEEDS_DIR="${SEEDS_DIR:-$(latest "$REPO"/../SEAM-CURE/seam-v*/seeds/)}"
XOLU_BIN="${XOLU_BIN:-}"

PORT=9191
SEED=01
RESET=false
DO_SEED=true
CMD=start

while [ $# -gt 0 ]; do
  case "$1" in
    stop|status) CMD="$1" ;;
    --port)      PORT="${2:?falta el número de puerto}"; shift ;;
    --seed)      SEED="${2:?falta el número de semilla}"; shift ;;
    --reset)     RESET=true ;;
    --no-seed)   DO_SEED=false ;;
    -h|--help)   sed -n '2,15p' "${BASH_SOURCE[0]}"; exit 0 ;;
    *) echo "Opción desconocida: $1 (usa --help)" >&2; exit 2 ;;
  esac
  shift
done

BIN="${XOLU_BIN:-$STATE/xolu}"
DATA="$STATE/data-$PORT"
VENV="$STATE/venv"
PIDFILE="$STATE/xolu-$PORT.pid"
LOG="$STATE/xolu-$PORT.log"
URL="http://127.0.0.1:$PORT"

say() { printf '\033[1m==>\033[0m %s\n' "$*"; }
die() { printf '\033[31mError:\033[0m %s\n' "$*" >&2; exit 1; }

is_running() {
  [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null
}

stop_xolu() {
  if is_running; then
    say "Apagando xolu (PID $(cat "$PIDFILE"))"
    kill "$(cat "$PIDFILE")" 2>/dev/null || true
    for _ in 1 2 3 4 5 6 7 8 9 10; do is_running || break; sleep 0.5; done
    is_running && kill -9 "$(cat "$PIDFILE")" 2>/dev/null || true
  else
    say "No hay un xolu lanzado por este script en el puerto $PORT"
  fi
  rm -f "$PIDFILE"
}

case "$CMD" in
  stop)   stop_xolu; exit 0 ;;
  status)
    if is_running; then say "Corriendo en $URL (PID $(cat "$PIDFILE"))"; else say "Apagado"; fi
    exit 0 ;;
esac

if [ -z "$XOLU_BIN" ] && [ ! -x "$BIN" ]; then
  [ -n "$XOLU_SRC" ] && [ -d "$XOLU_SRC/cmd/xolu" ] \
    || die "no encuentro el código de xolu; indica XOLU_SRC=/ruta/a/xolu o XOLU_BIN=/ruta/al/binario"
fi
SEED_FILE=""
if $DO_SEED; then
  [ -n "$SEEDS_DIR" ] || die "no encuentro las semillas de Seam; indica SEEDS_DIR=/ruta/a/seam/seeds o usa --no-seed"
  SEED_FILE="$(ls "$SEEDS_DIR"/seed-"$SEED"-*.py 2>/dev/null | head -1 || true)"
  [ -n "$SEED_FILE" ] || die "no encuentro la semilla $SEED en $SEEDS_DIR"
fi

if curl -s -m 2 -o /dev/null "$URL/ready" && ! is_running; then
  die "el puerto $PORT ya responde y no lo lanzó este script; elige otro con --port"
fi

mkdir -p "$STATE"

if [ -z "$XOLU_BIN" ] && [ ! -x "$BIN" ]; then
  say "Compilando xolu desde $XOLU_SRC (la primera vez tarda)"
  command -v go >/dev/null || die "hace falta Go para compilar xolu"
  (cd "$XOLU_SRC" && go build -o "$BIN" ./cmd/xolu)
fi

if $DO_SEED && [ ! -x "$VENV/bin/python" ]; then
  say "Creando entorno de Python con bcrypt y requests"
  python3 -m venv "$VENV"
  "$VENV/bin/pip" install -q bcrypt requests
fi

if $RESET; then
  stop_xolu
  say "Borrando datos de $DATA"
  rm -rf "$DATA"
fi

if is_running; then
  say "xolu ya estaba corriendo en $URL (PID $(cat "$PIDFILE"))"
else
  mkdir -p "$DATA"
  say "Arrancando xolu en $URL"
  XOLU_API_V2_ENABLED=true XOLU_CAL_ENABLED=true \
  XOLU_AUTH_TYPE=none XOLU_METRICS_ENABLED=false \
    nohup "$BIN" --host 127.0.0.1 --port "$PORT" --base-dir "$DATA" --no-ascii \
    >"$LOG" 2>&1 &
  echo $! >"$PIDFILE"
fi

for _ in $(seq 1 40); do
  curl -s -m 2 -o /dev/null "$URL/ready" && break
  is_running || { tail -20 "$LOG" >&2; die "xolu se cerró al arrancar (log: $LOG)"; }
  sleep 0.5
done
curl -s -m 2 "$URL/ready" >/dev/null || die "xolu no respondió en $URL (log: $LOG)"

MARK="$DATA/.seeded-$SEED"
if $DO_SEED; then
  if [ -f "$MARK" ]; then
    say "La semilla $SEED ya estaba cargada (usa --reset para recargar)"
  else
    say "Cargando semilla $SEED ($(basename "$SEED_FILE"))"
    "$VENV/bin/python" -I "$SEED_FILE" --url "$URL" >"$STATE/seed-$PORT.log" 2>&1 \
      || { tail -15 "$STATE/seed-$PORT.log" >&2; die "falló la semilla (log: $STATE/seed-$PORT.log)"; }
    touch "$MARK"
    sed -n '/Entities created:/,$p' "$STATE/seed-$PORT.log"
  fi
fi

cat <<EOF

xolu listo en $URL   (PID $(cat "$PIDFILE"))
  log:      $LOG
  apagar:   $0 stop --port $PORT

Para probar molu contra este xolu:
  MOLU_FRONT_XOLU_URL=$URL MOLU_FRONT_XOLU_AUTH_MODE=bearer MOLU_FRONT_XOLU_TOKEN_FILE=<archivo con un token> ./bin/molu
  make inspector XOLU_URL=$URL
EOF
