# Timeline Token × Radar Democracia

Motor de descubrimiento y seguimiento cronológico de noticias en Go con extracción multicapa (SearXNG + RSS/Atom + Sitemaps + navegador headless vía MCP), extracción de **relaciones dirigidas con `hugot-gliner2`**, integración nativa con **YouTube Data API (`democracia` / Talkback Outlier Score)**, **dashboard dual (API `:8085` + visor estático `.db.gz` con SQLite WASM en GitHub Pages)** y **automatización diaria con `cron` de Linux**.

👉 **[Dashboard en vivo (GitHub Pages — modo WASM `.db.gz`)](https://estoyqueloleo-max.github.io/timeline-token/index.html)**

---

## Arquitectura

1. **Descubrimiento y Extracción Híbrida (`crawler.go`, `extractor.go`, `mcp_bridge.go`)**:
   - Descubrimiento en **SearXNG** (`http://localhost:8080`) con control de `--delay` y filtros por `--regions`.
   - Captura por **RSS/Atom**, **Sitemaps de noticias (`robots.txt`, `sitemap-news.xml`)** y scraping de portada/hemeroteca.
   - **Fallback determinista vía MCP (`mcp-web-actor`)**: cuando un portal falla por HTTP o devuelve `0` enlaces (p. ej. renderizado JS o bloqueos), `MCPBridge` invoca por `stdio` al navegador headless (`extract_structured_news`) aplicando capas DOM -> JSON-LD -> OpenGraph.
   - Diagnóstico estructurado de cobertura y errores en `diagnostics.go` (`crawl_diagnostics.json`).

2. **Timeline Cronológico de Conceptos (`concepts_timeline.go`)**:
   - `NormalizeEventDate` unifica fechas heterogéneas al día de evento `YYYY-MM-DD`.
   - `BuildDailyConcepts` agrupa titulares por fecha, nº de portales distintos, menciones, indicador político/institucional (`is_political`) y **Burst Score**.

3. **Extracción Nativa de Relaciones con `hugot-gliner2` (`gliner_relations.go`)**:
   - Integrado directamente como módulo Go (`replace hugot-gliner2 => ../hugot-gliner2`) sobre ONNX Runtime.
   - Usa un esquema predefinido de 20 relaciones políticas, institucionales y económicas (`DefaultNewsRelationLabels`, personalizable en `relations_schema.json` mediante `LoadRelationSchemaLabels`), almacenando tripletas `(head_entity, relation, tail_entity)` con confianza en la tabla `news_relations`.

4. **Radar YouTube & Método Talkback (`youtube_democracia.go`)**:
   - Usa las librerías oficiales de Go (`google.golang.org/api/youtube/v3` y `golang.org/x/oauth2/google`) con las credenciales OAuth del proyecto hermano `/home/jose/workspace/sandbox/youtube/democracia` (`client_secret.json` y `youtube.token.json`, con refresco persistente).
   - Consulta vídeos recientes de los conceptos principales del día, recupera estadísticas de vídeo y canal y calcula el **Outlier Ratio (`views / subscribers`)** para detectar temas emergentes (`>= 5x`).

5. **Dashboard Dual + Snapshot `.db.gz` para GitHub Pages (`api_server.go`, `visualization.go`, `db/db.go`)**:
   - Genera `semantic_map.html` con 3 vistas: **Timeline Cronológico**, **Radar YouTube & Democracia** y **Grafo D3 (GLiNER2 + Co-ocurrencia)**.
   - **Modo API Local (`--serve` en `:8085`)**: sirve `/api/overview`, `/api/timeline`, `/api/relations`, `/api/youtube`, `/api/graph` y `POST /api/daily-run`.
   - **Modo Estático / GitHub Pages (SQLite WASM + Gzip)**: `GenerateVisualization()` ejecuta `db.CompressDBSnapshot("news_central.db", "news_central.db.gz")` (`~43.5 MB` -> `~12.5 MB`). Cuando `/api/overview` no responde (GitHub Pages) o al pulsar **📦 Visor .db.gz (WASM)**, el navegador descarga `news_central.db.gz`, lo descomprime en memoria con `DecompressionStream('gzip')` y lo consulta directamente mediante `sql.js`.
   - Además, `db.InitDB()` restaura automáticamente `news_central.db` desde `news_central.db.gz` si no existe en disco al arrancar.

---

## Opciones CLI (`news-tool`)

| Flag | Descripción | Por defecto |
|------|-------------|-------------|
| `--serve` | Inicia el servidor HTTP local del dashboard agregado y regenera `semantic_map.html` + `news_central.db.gz`. | `false` |
| `--port` | Puerto del servidor HTTP local. | `8085` |
| `--check-today` | Comprobación de solo lectura de `daily_runs` (`exit 0` si hoy ya se completó, `exit 1` si está pendiente). | `false` |
| `--daily-check` | Valida `daily_runs` para la fecha actual y ejecuta la pasada diaria completa solo si aún no se completó hoy. | `false` |
| `--daily-run` | Fuerza la pasada diaria completa (crawl incremental + conceptos + GLiNER2 + YouTube). | `false` |
| `--rebuild-concepts` | Recalcula conceptos cronológicos, relaciones GLiNER2 pendientes y enriquecimiento YouTube sin recrawlear portales. | `false` |
| `--gliner-dir` | Directorio con los assets ONNX/tokenizer de `hugot-gliner2`. | `/home/jose/workspace/sandbox/gemini/hugot-gliner2` |
| `--democracia-dir` | Directorio del proyecto `democracia` con credenciales OAuth de YouTube. | `/home/jose/workspace/sandbox/youtube/democracia` |
| `--gliner-limit` | Máximo de artículos candidatos procesados por `hugot-gliner2` en cada pasada. | `45` |
| `--yt-top` | Top de conceptos diarios a consultar automáticamente en YouTube Data API v3. | `6` |
| `--use-mcp` | Habilita el fallback con navegador MCP (`mcp-web-actor`). | `true` |
| `--mcp-bin` | Ruta explícita al binario `mcp-web-actor`. | `""` |
| `--mode` | Modo del pipeline clásico: `live`, `historical` o `both`. | `live` |
| `--resume` | Omite descubrimiento y análisis sobre datos ya existentes. | `false` |
| `--delay` | Pausa entre consultas en SearXNG (ej. `300ms`, `5s`). | `1s` |
| `--regions` | Filtro por regiones separado por comas (`es,global,fr,de,ar,latam`). | `""` |
| `--skip-analysis` | Omite detección de hemerotecas con LLM y bucle clásico de DistilBERT. | `false` |
| `--llm` | Endpoint LLM local compatible con OpenAI para detección de hemerotecas. | `http://192.168.1.4:4001` |
| `--model` | Nombre del modelo LLM local. | `qwen3` |
| `--test-archive-url` | Prueba la detección de hemeroteca para una URL concreta. | `""` |

---

## Gestión de Servicios y Cron Automático (`start-services.sh`)

```bash
./start-services.sh [comando]
```

- `start`: Verifica/arranca el contenedor `searxng` en `:8080` sin colisionar si ya existe, valida binarios MCP en `/home/jose/go/bin` (`mcp-web-actor`, `mcp-searxng`, `mcp-news`) y compila `news-tool`.
- `serve`: Compila `news-tool` si hace falta y levanta `--serve --port=8085` en segundo plano.
- `daily-run`: Ejecuta una pasada diaria completa (`--daily-run --delay=300ms`) y sincroniza la BD con Git (`sync_db_git`).
- `rebuild-concepts`: Recalcula conceptos + GLiNER2 + YouTube (`--rebuild-concepts`) sobre la BD local y ejecuta `sync_db_git`.
- `sync-db`: Hace `git commit --only` de `news_central.db.gz` y `semantic_map.html` (si cambiaron) y `git push origin main` para desplegar en GitHub Pages (`AUTO_GIT_SYNC=0` lo desactiva).
- `cron-install`: Instala en el `crontab` del usuario las entradas `@reboot` y `5 * * * *` apuntando a `cron-check`.
- `cron-check`: Validador idempotente usado por `cron` (candado `flock` en `/tmp/timeline-token-daily.lock`, levanta `:8085`, valida `--check-today` para no repetir pasadas en el mismo día, ejecuta `--daily-check` si está pendiente y corre `sync_db_git`).
- `cron-status` / `cron-remove`: Muestra el estado del `crontab` e historial de `daily_runs` en SQLite, o elimina las entradas de `crontab`.
- `status` / `stop [--all]` / `restart` / `logs`: Inspección y control de `:8085`, `searxng`, logs locales y cron.

---

## Inicio Rápido

```bash
# 1. Compilar y pasar tests unitarios
go build -o news-tool .
go test -v ./...

# 2. Arrancar dependencias y dashboard en http://localhost:8085/semantic_map.html
./start-services.sh start
./start-services.sh serve

# 3. Instalar automatización diaria al encender el equipo (con validador + auto-commit/push)
./start-services.sh cron-install
./start-services.sh cron-status
```

---

## Persistencia SQLite (`db/schema.sql`, `db/db.go`)

`news_central.db` se mantiene ignorada en Git y respaldada mediante [news_central.db.gz](news_central.db.gz) (`deploy.yml` publica el snapshot `.db.gz` y `semantic_map.html` como `index.html` en GitHub Pages):

- **`portals`**: Registro de medios (`domain`, URL, RSS, región, idioma, hemeroteca).
- **`news`**: Noticias extraídas con hash único (`news_hash`), fecha normalizable, resumen e indicador histórico.
- **`daily_concepts`**: Conceptos agregados por fecha (`event_date`), nº de portales, menciones, `burst_score`, titulares de ejemplo y relaciones vinculadas.
- **`news_relations`**: Tripletas dirigidas `(head_entity, relation, tail_entity)` y confianzas extraídas por `hugot-gliner2`.
- **`youtube_concept_videos`**: Vídeos por concepto con visitas, suscriptores de canal, likes, comentarios y `outlier_ratio`.
- **`daily_runs`**: Registro de ejecuciones diarias (`run_date`, `status`, contadores y notas) usado por `--check-today` y `cron-check`.
- **`news_entities` / `news_embeddings` / `news_clusters`**: Tablas del analizador clásico (NER / embeddings / clusters).
