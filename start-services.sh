#!/usr/bin/env bash
set -euo pipefail

export PATH="/usr/local/go/bin:/home/jose/go/bin:${HOME}/.local/bin:${PATH}"

GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MCP_DIR="${MCP_DIR:-/home/jose/workspace/sandbox/gemini/mcp-webdriverio}"
SEARXNG_DIR="${SEARXNG_DIR:-/home/jose/open-notebook/searchng}"
CONTAINER_NAME="${SEARXNG_CONTAINER_NAME:-searxng}"
SEARXNG_PORT="${SEARXNG_PORT:-8080}"
WEB_PORT="${WEB_PORT:-8085}"
LLM_URL="${LLM_URL:-http://192.168.1.4:4001}"
INSTALL_BIN_DIR="${INSTALL_BIN_DIR:-/home/jose/go/bin}"
SERVE_PID_FILE="${SCRIPT_DIR}/.timeline-serve.pid"
SERVE_LOG_FILE="${SCRIPT_DIR}/timeline-serve.log"
NEWS_TOOL_BIN="${SCRIPT_DIR}/news-tool"
CRON_LOCK_FILE="/tmp/timeline-token-daily.lock"
CRON_LOG_FILE="/tmp/timeline-token-cron.log"
CRON_MARKER="# timeline-token-daily-autocheck"
AUTO_GIT_SYNC="${AUTO_GIT_SYNC:-1}"
AUTO_STOP_SEARXNG="${AUTO_STOP_SEARXNG:-1}"

usage() {
    echo "Uso: $0 [start|stop|stop-searxng|restart|status|serve|logs|daily-run|rebuild-concepts|sync-db|cron-check|cron-install|cron-status|cron-remove]"
    echo ""
    echo "Comandos:"
    echo "  start             Valida o arranca SearXNG (:${SEARXNG_PORT}), MCP y compila news-tool"
    echo "  serve             Compila (si hace falta) y arranca el dashboard agregado en :${WEB_PORT}"
    echo "  daily-run         Fuerza una pasada diaria completa (crawl + conceptos + GLiNER2 + YouTube)"
    echo "  rebuild-concepts  Recalcula conceptos cronológicos, GLiNER2 y YouTube sobre la BD actual"
    echo "  sync-db           Hace commit (--only) y push de news_central.db.gz y semantic_map.html a GitHub"
    echo "  cron-check        Validador idempotente para cron (flock + :${WEB_PORT} + --check-today + sync-db)"
    echo "  cron-install      Instala @reboot y chequeo horario en crontab del usuario"
    echo "  cron-status       Muestra las entradas cron instaladas y el historial daily_runs"
    echo "  cron-remove       Elimina las entradas de timeline-token del crontab"
    echo "  stop [--all|--searxng] Detiene el servidor web :${WEB_PORT} (o SearXNG con --searxng/--all)"
    echo "  stop-searxng      Detiene el contenedor SearXNG (:${SEARXNG_PORT}) sin borrarlo"
    echo "  restart           Reinicia servicios"
    echo "  status            Muestra el estado completo de servicios, BD y automatización cron"
    echo "  logs              Sigue los logs locales, de cron y de SearXNG"
    echo ""
    exit 1
}

container_exists() {
    docker ps -a --filter "name=^/${CONTAINER_NAME}$" --format '{{.Names}}' | grep -qx "${CONTAINER_NAME}"
}

container_running() {
    docker ps --filter "name=^/${CONTAINER_NAME}$" --format '{{.Names}}' | grep -qx "${CONTAINER_NAME}"
}

searxng_http_code() {
    local code
    code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 2 "http://localhost:${SEARXNG_PORT}/search?q=test&format=json" || true)
    echo "${code:-000}"
}

wait_for_searxng() {
    local max_retries=20
    local retry=0
    local check_url="http://localhost:${SEARXNG_PORT}/search?q=test&format=json"

    echo -ne "${YELLOW}Esperando a que SearXNG responda en ${check_url}... ${NC}"
    while [ "${retry}" -lt "${max_retries}" ]; do
        if [ "$(searxng_http_code)" = "200" ]; then
            echo -e "${GREEN}✓ Listo (HTTP 200)${NC}"
            return 0
        fi
        sleep 1
        retry=$((retry + 1))
    done

    echo -e "${RED}TIMEOUT esperando HTTP 200 en SearXNG${NC}"
    return 1
}

port_in_use() {
    local port="$1"
    if command -v ss >/dev/null 2>&1; then
        ss -ltn 2>/dev/null | grep -qE ":${port}\b"
    else
        curl -s -o /dev/null --max-time 1 "http://localhost:${port}/" 2>/dev/null
    fi
}

ensure_searxng() {
    echo -e "${BLUE}=== Verificando SearXNG (puerto ${SEARXNG_PORT}) ===${NC}"
    if [ "$(searxng_http_code)" = "200" ]; then
        echo -e "${GREEN}✓ SearXNG ya está levantado en :${SEARXNG_PORT} (reutilizando servicio compartido).${NC}"
        return 0
    fi

    if container_running; then
        echo -e "${YELLOW}Contenedor '${CONTAINER_NAME}' corriendo pero aún no responde HTTP 200; esperando...${NC}"
        wait_for_searxng || true
        return 0
    fi

    local other_container
    other_container=$(docker ps --filter "publish=${SEARXNG_PORT}" --format '{{.Names}}' | head -n 1 || true)
    if [ -n "${other_container}" ]; then
        echo -e "${YELLOW}⚠ El puerto ${SEARXNG_PORT} ya está ocupado por el contenedor '${other_container}'. No se creará otro contenedor para evitar colisión.${NC}"
        return 0
    fi

    if port_in_use "${SEARXNG_PORT}"; then
        echo -e "${YELLOW}⚠ El puerto ${SEARXNG_PORT} ya está en uso en el host. Se omite docker run para evitar colisión.${NC}"
        return 0
    fi

    if container_exists; then
        echo -e "${YELLOW}El contenedor '${CONTAINER_NAME}' existía detenido. Arrancando...${NC}"
        docker start "${CONTAINER_NAME}" >/dev/null
        wait_for_searxng || true
        return 0
    fi

    if [ ! -f "${SEARXNG_DIR}/settings.yml" ]; then
        echo -e "${RED}Error: No se encontró ${SEARXNG_DIR}/settings.yml para iniciar SearXNG.${NC}" >&2
        exit 1
    fi

    echo -e "${BLUE}Creando y arrancando contenedor '${CONTAINER_NAME}' en puerto ${SEARXNG_PORT}...${NC}"
    docker run -d \
        --name "${CONTAINER_NAME}" \
        -p "${SEARXNG_PORT}:8080" \
        -v "${SEARXNG_DIR}/settings.yml:/etc/searxng/settings.yml" \
        searxng/searxng >/dev/null
    echo -e "${GREEN}✓ Contenedor '${CONTAINER_NAME}' arrancado.${NC}"
    wait_for_searxng || true
}

stop_searxng() {
    if container_running; then
        echo -e "${YELLOW}Deteniendo contenedor SearXNG '${CONTAINER_NAME}' (puerto ${SEARXNG_PORT})...${NC}"
        docker stop "${CONTAINER_NAME}" >/dev/null 2>&1 || true
        echo -e "${GREEN}✓ SearXNG detenido.${NC}"
    else
        echo -e "${YELLOW}El contenedor '${CONTAINER_NAME}' no está corriendo.${NC}"
    fi
}

mcp_needs_rebuild() {
    local web_bin="${INSTALL_BIN_DIR}/mcp-web-actor"
    if [ ! -x "${web_bin}" ] || [ ! -x "${INSTALL_BIN_DIR}/mcp-searxng" ] || [ ! -x "${INSTALL_BIN_DIR}/mcp-news" ]; then
        return 0
    fi
    if [ -d "${MCP_DIR}" ]; then
        if [ "${MCP_DIR}/main.go" -nt "${web_bin}" ] || \
           [ "${MCP_DIR}/mcp-searxng/main.go" -nt "${INSTALL_BIN_DIR}/mcp-searxng" ] || \
           [ "${MCP_DIR}/mcp-news/main.go" -nt "${INSTALL_BIN_DIR}/mcp-news" ]; then
            return 0
        fi
    fi
    return 1
}

ensure_mcp_binaries() {
    echo -e "\n${BLUE}=== Verificando binarios MCP (${INSTALL_BIN_DIR}) ===${NC}"
    if mcp_needs_rebuild; then
        if [ -x "${MCP_DIR}/start-services.sh" ]; then
            echo -e "${YELLOW}Binarios MCP faltantes o desactualizados. Compilando vía ${MCP_DIR}/start-services.sh...${NC}"
            "${MCP_DIR}/start-services.sh" start
        else
            echo -e "${RED}✗ Faltan binarios MCP en ${INSTALL_BIN_DIR} y no se encontró ${MCP_DIR}/start-services.sh.${NC}" >&2
            return 1
        fi
    else
        echo -e "${GREEN}✓ Binarios MCP actualizados en ${INSTALL_BIN_DIR} (reutilizando mcp-web-actor, mcp-searxng, mcp-news).${NC}"
    fi
}

build_news_tool() {
    echo -e "\n${BLUE}=== Verificando binario timeline-token (news-tool) ===${NC}"
    local needs_build=0
    if [ ! -x "${NEWS_TOOL_BIN}" ]; then
        needs_build=1
    elif [ -n "$(find "${SCRIPT_DIR}" -maxdepth 2 \( -name '*.go' -o -name 'go.mod' \) -newer "${NEWS_TOOL_BIN}" 2>/dev/null)" ]; then
        needs_build=1
    fi

    if [ "${needs_build}" -eq 1 ]; then
        echo -e "${BLUE}Compilando news-tool en ${SCRIPT_DIR}...${NC}"
        (cd "${SCRIPT_DIR}" && go build -o "${NEWS_TOOL_BIN}" .)
        echo -e "${GREEN}✓ news-tool compilado.${NC}"
    else
        echo -e "${GREEN}✓ news-tool ya está actualizado.${NC}"
    fi
}

is_serve_running() {
    if [ -f "${SERVE_PID_FILE}" ]; then
        local pid
        pid=$(cat "${SERVE_PID_FILE}" 2>/dev/null || true)
        if [ -n "${pid}" ] && kill -0 "${pid}" 2>/dev/null; then
            return 0
        fi
    fi
    return 1
}

start_serve() {
    build_news_tool
    echo -e "\n${BLUE}=== Servidor web Dashboard Agregado (puerto ${WEB_PORT}) ===${NC}"
    if is_serve_running; then
        echo -e "${GREEN}✓ Ya está en ejecución (PID $(cat "${SERVE_PID_FILE}")): http://localhost:${WEB_PORT}/semantic_map.html${NC}"
        return 0
    fi
    if port_in_use "${WEB_PORT}"; then
        echo -e "${YELLOW}⚠ El puerto ${WEB_PORT} ya está ocupado en el host. No se arrancará otro --serve para evitar colisión.${NC}"
        return 0
    fi

    (
        cd "${SCRIPT_DIR}"
        nohup "${NEWS_TOOL_BIN}" --serve --port="${WEB_PORT}" > "${SERVE_LOG_FILE}" 2>&1 200>&- < /dev/null &
        echo $! > "${SERVE_PID_FILE}"
    )
    local pid
    pid=$(cat "${SERVE_PID_FILE}" 2>/dev/null || true)
    sleep 1
    if [ -n "${pid}" ] && kill -0 "${pid}" 2>/dev/null; then
        echo -e "${GREEN}✓ Servidor iniciado (PID ${pid}): http://localhost:${WEB_PORT}/semantic_map.html${NC}"
    else
        rm -f "${SERVE_PID_FILE}"
        echo -e "${RED}✗ Error arrancando --serve; revisa ${SERVE_LOG_FILE}${NC}" >&2
        return 1
    fi
}

stop_serve() {
    if is_serve_running; then
        local pid
        pid=$(cat "${SERVE_PID_FILE}")
        echo -e "${YELLOW}Deteniendo servidor web timeline-token (PID ${pid})...${NC}"
        kill "${pid}" 2>/dev/null || true
        rm -f "${SERVE_PID_FILE}"
        echo -e "${GREEN}✓ Servidor web detenido.${NC}"
    else
        rm -f "${SERVE_PID_FILE}"
        echo -e "${YELLOW}El servidor web (:${WEB_PORT}) no estaba corriendo.${NC}"
    fi
}

sync_db_git() {
    if [ "${AUTO_GIT_SYNC}" != "1" ]; then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] AUTO_GIT_SYNC=0: omitiendo commit/push automático de BD."
        return 0
    fi
    if ! git -C "${SCRIPT_DIR}" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Aviso: ${SCRIPT_DIR} no es un repositorio Git."
        return 0
    fi

    local changed
    changed=$(git -C "${SCRIPT_DIR}" status --porcelain -- news_central.db.gz semantic_map.html 2>/dev/null || true)
    if [ -n "${changed}" ]; then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Detectados cambios en snapshot DB/visor. Preparando commit..."
        git -C "${SCRIPT_DIR}" add -- news_central.db.gz semantic_map.html

        if ! git -C "${SCRIPT_DIR}" diff --cached --quiet -- news_central.db.gz semantic_map.html; then
            local summary=""
            if [ -f "${SCRIPT_DIR}/news_central.db" ] && command -v python3 >/dev/null 2>&1; then
                summary=$(python3 -c '
import sqlite3
try:
    conn = sqlite3.connect("'"${SCRIPT_DIR}/news_central.db"'")
    row = conn.execute("SELECT run_date, COALESCE(news_added,0), COALESCE(concepts_extracted,0), COALESCE(relations_extracted,0), COALESCE(youtube_videos_fetched,0) FROM daily_runs ORDER BY run_date DESC LIMIT 1").fetchone()
    if row:
        print(f"({row[0]}: news+{row[1]}, concepts={row[2]}, gliner={row[3]}, yt={row[4]})")
except Exception:
    pass
' 2>/dev/null || true)
            fi
            local today_str commit_msg
            today_str=$(date '+%Y-%m-%d')
            commit_msg="chore(data): actualización diaria BD ${today_str}"
            if [ -n "${summary}" ]; then
                commit_msg="${commit_msg} ${summary}"
            fi

            git -C "${SCRIPT_DIR}" commit --only -m "${commit_msg}" -- news_central.db.gz semantic_map.html
            echo "[$(date '+%Y-%m-%d %H:%M:%S')] ✓ Commit creado: ${commit_msg}"
        fi
    fi

    local ahead=0
    ahead=$(git -C "${SCRIPT_DIR}" rev-list --count origin/main..HEAD 2>/dev/null || echo "0")
    if [ "${ahead}" -gt 0 ]; then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Haciendo git push origin main (${ahead} commit(s) pendiente(s))..."
        if git -C "${SCRIPT_DIR}" push origin main; then
            echo "[$(date '+%Y-%m-%d %H:%M:%S')] ✓ Snapshot subido a origin main (despliegue GitHub Pages activado)."
        else
            echo "[$(date '+%Y-%m-%d %H:%M:%S')] ⚠ No se pudo hacer git push ahora (se reintentará en el próximo chequeo)."
        fi
    else
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Git al día con origin/main (sin cambios nuevos de BD que subir)."
    fi
}

run_daily() {
    ensure_searxng
    ensure_mcp_binaries >/dev/null || true
    build_news_tool
    echo -e "\n${BLUE}=== Ejecutando pasada diaria completa (--daily-run) ===${NC}"
    (cd "${SCRIPT_DIR}" && SEARXNG_URL="http://localhost:${SEARXNG_PORT}" "${NEWS_TOOL_BIN}" --daily-run --delay=300ms) || true
    if [ "${AUTO_STOP_SEARXNG}" = "1" ]; then
        echo -e "\n${BLUE}=== Apagando SearXNG tras finalizar pasada diaria ===${NC}"
        stop_searxng
    fi
    sync_db_git
}

run_rebuild_concepts() {
    build_news_tool
    echo -e "\n${BLUE}=== Recalculando Conceptos + GLiNER2 + YouTube (--rebuild-concepts) ===${NC}"
    (cd "${SCRIPT_DIR}" && "${NEWS_TOOL_BIN}" --rebuild-concepts)
    sync_db_git
}

cron_check() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] Iniciando cron-check..."
    build_news_tool >/dev/null

    exec 200>"${CRON_LOCK_FILE}"
    if ! flock -n 200; then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Ya hay otro cron-check en ejecución. Saliendo."
        return 0
    fi

    # Wait briefly after reboot for Docker daemon / SearXNG
    if ! docker info >/dev/null 2>&1; then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Esperando al demonio de Docker..."
        sleep 10
    fi

    if (cd "${SCRIPT_DIR}" && "${NEWS_TOOL_BIN}" --check-today >/dev/null 2>&1); then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Validación OK: hoy ya está completado en daily_runs."
        sync_db_git
        return 0
    fi

    ensure_searxng
    ensure_mcp_binaries >/dev/null || true
    (cd "${SCRIPT_DIR}" && SEARXNG_URL="http://localhost:${SEARXNG_PORT}" "${NEWS_TOOL_BIN}" --daily-check --delay=300ms) || true
    if [ "${AUTO_STOP_SEARXNG}" = "1" ]; then
        echo "[$(date '+%Y-%m-%d %H:%M:%S')] Apagando SearXNG tras finalizar pasada para liberar puerto ${SEARXNG_PORT}..."
        stop_searxng
    fi
    sync_db_git
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] cron-check finalizado."
}

cron_install() {
    local reboot_line="@reboot ${SCRIPT_DIR}/start-services.sh cron-check >> ${CRON_LOG_FILE} 2>&1 ${CRON_MARKER}"
    local hourly_line="5 * * * * ${SCRIPT_DIR}/start-services.sh cron-check >> ${CRON_LOG_FILE} 2>&1 ${CRON_MARKER}"

    local existing cleaned
    existing=$(crontab -l 2>/dev/null || true)
    cleaned=$(printf "%s\n" "${existing}" | grep -v "${CRON_MARKER}" || true)

    {
        if [ -n "${cleaned}" ]; then
            printf "%s\n" "${cleaned}"
        fi
        printf "%s\n" "${reboot_line}"
        printf "%s\n" "${hourly_line}"
    } | crontab -

    echo -e "${GREEN}✓ Entradas cron instaladas para el usuario $(whoami):${NC}"
    echo "  ${reboot_line}"
    echo "  ${hourly_line}"
}

cron_remove() {
    local existing cleaned
    existing=$(crontab -l 2>/dev/null || true)
    cleaned=$(printf "%s\n" "${existing}" | grep -v "${CRON_MARKER}" || true)
    if [ -n "${cleaned}" ]; then
        printf "%s\n" "${cleaned}" | crontab -
    else
        crontab -r 2>/dev/null || true
    fi
    echo -e "${GREEN}✓ Entradas cron de timeline-token eliminadas.${NC}"
}

cron_status() {
    echo -e "${BLUE}=== Crontab Usuario ($(whoami)) ===${NC}"
    local lines
    lines=$(crontab -l 2>/dev/null | grep "${CRON_MARKER}" || true)
    if [ -n "${lines}" ]; then
        echo -e "${GREEN}${lines}${NC}"
    else
        echo -e "${YELLOW}No hay entradas cron de timeline-token instaladas (instala con: $0 cron-install)${NC}"
    fi

    echo -e "\n${BLUE}=== Historial daily_runs (SQLite) ===${NC}"
    if [ -f "${SCRIPT_DIR}/news_central.db" ] && command -v python3 >/dev/null 2>&1; then
        python3 -c '
import sqlite3
conn = sqlite3.connect("'"${SCRIPT_DIR}/news_central.db"'")
cur = conn.cursor()
try:
    rows = cur.execute("SELECT run_date, status, COALESCE(finished_at, started_at, \"\"), news_added, concepts_extracted, relations_extracted, youtube_videos_fetched FROM daily_runs ORDER BY run_date DESC LIMIT 7").fetchall()
    if not rows:
        print("Sin registros aún en daily_runs.")
    for r in rows:
        print(f"  {r[0]} | {r[1]:9s} | fin={r[2]} | news+{r[3]} | concepts={r[4]} | gliner={r[5]} | yt={r[6]}")
except Exception as e:
    print("Tabla daily_runs aún no creada:", e)
'
    fi
}

start_services() {
    ensure_searxng
    ensure_mcp_binaries
    build_news_tool
    echo ""
    show_status
}

stop_services() {
    local stop_opt="${1:-}"
    if [ "${stop_opt}" = "--searxng" ]; then
        stop_searxng
        return 0
    fi

    stop_serve

    if [ "${stop_opt}" = "--all" ]; then
        echo -e "${YELLOW}Deteniendo contenedor compartido '${CONTAINER_NAME}' (--all)...${NC}"
        stop_searxng
        if container_exists; then
            docker rm "${CONTAINER_NAME}" >/dev/null 2>&1 || true
            echo -e "${GREEN}✓ Contenedor '${CONTAINER_NAME}' eliminado.${NC}"
        fi
    else
        if container_running || [ "$(searxng_http_code)" = "200" ]; then
            echo -e "${BLUE}ℹ Contenedor 'searxng' (:${SEARXNG_PORT}) se mantiene activo (usa '$0 stop --all' o '$0 stop-searxng' para apagarlo).${NC}"
        fi
    fi
}

restart_services() {
    local opt="${1:-}"
    local was_serving=0
    if is_serve_running; then
        was_serving=1
    fi
    stop_services "${opt}"
    start_services
    if [ "${was_serving}" -eq 1 ]; then
        start_serve
    fi
}

show_status() {
    echo -e "${BLUE}=== Estado SearXNG (:${SEARXNG_PORT}) ===${NC}"
    if container_exists; then
        docker ps -a --filter "name=^/${CONTAINER_NAME}$" --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"
    else
        echo "Contenedor '${CONTAINER_NAME}': no existe en Docker local"
    fi
    local code
    code=$(searxng_http_code)
    if [ "${code}" = "200" ]; then
        echo -e "${GREEN}✓ http://localhost:${SEARXNG_PORT}/search?q=test&format=json -> HTTP ${code}${NC}"
    else
        echo -e "${RED}✗ http://localhost:${SEARXNG_PORT}/search?q=test&format=json -> HTTP ${code}${NC}"
    fi

    echo -e "\n${BLUE}=== Binarios MCP Globales (${INSTALL_BIN_DIR}) ===${NC}"
    for bin_name in "mcp-web-actor" "mcp-searxng" "mcp-news"; do
        if [ -x "${INSTALL_BIN_DIR}/${bin_name}" ]; then
            echo -e "${GREEN}✓ ${INSTALL_BIN_DIR}/${bin_name} (listo)${NC}"
        else
            echo -e "${RED}✗ ${INSTALL_BIN_DIR}/${bin_name} (falta)${NC}"
        fi
    done

    echo -e "\n${BLUE}=== Proyecto timeline-token ===${NC}"
    if [ -x "${NEWS_TOOL_BIN}" ]; then
        echo -e "${GREEN}✓ ${NEWS_TOOL_BIN} (compilado)${NC}"
    else
        echo -e "${YELLOW}✗ ${NEWS_TOOL_BIN} (no compilado)${NC}"
    fi

    if is_serve_running; then
        echo -e "${GREEN}✓ Dashboard web corriendo (PID $(cat "${SERVE_PID_FILE}")): http://localhost:${WEB_PORT}/semantic_map.html${NC}"
    elif port_in_use "${WEB_PORT}"; then
        echo -e "${YELLOW}⚠ Puerto ${WEB_PORT} ocupado externamente (sin PID local)${NC}"
    else
        echo -e "${YELLOW}- Dashboard web detenido (arranca con: $0 serve)${NC}"
    fi

    echo -e ""
    cron_status

    echo -e "\n${BLUE}=== Endpoint LLM Opcional (${LLM_URL}) ===${NC}"
    local llm_code
    llm_code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 2 "${LLM_URL}/v1/models" || true)
    llm_code="${llm_code:-000}"
    if [ "${llm_code}" = "200" ]; then
        echo -e "${GREEN}✓ ${LLM_URL} -> HTTP ${llm_code}${NC}"
    else
        echo -e "${YELLOW}- ${LLM_URL} -> HTTP ${llm_code} (opcional; necesario solo si se ejecuta detección de hemerotecas con LLM)${NC}"
    fi
}

show_logs() {
    local pids=()
    if [ -f "${SERVE_LOG_FILE}" ]; then
        echo -e "${GREEN}Siguiendo ${SERVE_LOG_FILE}...${NC}"
        tail -n 30 -f "${SERVE_LOG_FILE}" &
        pids+=($!)
    fi
    if [ -f "${CRON_LOG_FILE}" ]; then
        echo -e "${GREEN}Siguiendo ${CRON_LOG_FILE}...${NC}"
        tail -n 30 -f "${CRON_LOG_FILE}" &
        pids+=($!)
    fi
    if [ -f "${INSTALL_BIN_DIR}/mcp-server.log" ]; then
        echo -e "${GREEN}Siguiendo ${INSTALL_BIN_DIR}/mcp-server.log...${NC}"
        tail -n 30 -f "${INSTALL_BIN_DIR}/mcp-server.log" &
        pids+=($!)
    fi
    if container_exists; then
        echo -e "${GREEN}Conectando a logs de Docker (${CONTAINER_NAME})...${NC}"
        docker logs -f --tail=30 "${CONTAINER_NAME}" &
        pids+=($!)
    fi

    if [ ${#pids[@]} -eq 0 ]; then
        echo -e "${YELLOW}No hay logs activos que mostrar.${NC}"
        return 0
    fi

    echo -e "${YELLOW}Pulsa Ctrl+C para salir de los logs.${NC}"
    trap 'kill "${pids[@]}" 2>/dev/null || true' INT TERM EXIT
    wait
}

ACTION="${1:-start}"

case "${ACTION}" in
    start)
        start_services
        ;;
    serve)
        ensure_mcp_binaries >/dev/null || true
        start_serve
        ;;
    daily-run)
        run_daily
        ;;
    rebuild-concepts)
        run_rebuild_concepts
        ;;
    sync-db)
        sync_db_git
        ;;
    cron-check)
        cron_check
        ;;
    cron-install)
        cron_install
        ;;
    cron-status)
        cron_status
        ;;
    cron-remove)
        cron_remove
        ;;
    stop-searxng)
        stop_searxng
        ;;
    stop)
        stop_services "${2:-}"
        ;;
    restart)
        restart_services "${2:-}"
        ;;
    status)
        show_status
        ;;
    logs)
        show_logs
        ;;
    *)
        usage
        ;;
esac

