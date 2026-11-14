# Roadmap & Estado - Timeline Token

## ✅ Completado

- **Fallback de Navegador vía MCP + Sitemaps (`mcp_bridge.go`, `crawler.go`)**: Soporte para `sitemap-news.xml` / `robots.txt` y conexión persistente `stdio` con `mcp-web-actor` (`extract_structured_news`) para medios con JS o bloqueos HTTP.
- **Diagnósticos de Cobertura (`diagnostics.go`)**: Registro estructurado (`crawl_diagnostics.json`) de portales exitosos, bloqueos y fallos por estrategia.
- **Integración Nativa `hugot-gliner2` (`gliner_relations.go`)**: Extracción de relaciones dirigidas hacia una lista configurable de relaciones (`DefaultNewsRelationLabels` / `relations_schema.json`) persistidas en `news_relations`.
- **Timeline Cronológico y Burst Detection (`concepts_timeline.go`)**: Normalización diaria (`YYYY-MM-DD`), conteo multi-portal, detección de picos (`burst_score`) y marcado de conceptos políticos/democracia.
- **Integración YouTube & Democracia (`youtube_democracia.go`)**: Conexión nativa con YouTube Data API v3 reutilizando credenciales de `youtube/democracia` y cálculo de **Outlier Ratio (`views / subscribers`)**.
- **Dashboard Dual Agregado (`visualization.go`, `api_server.go`)**: Tres vistas unificadas (**Timeline Cronológico**, **Radar YouTube & Democracia**, **Grafo D3 GLiNER2 + Co-ocurrencia**) en `:8085` + modo estático con SQLite WASM (`sql.js`).
- **Distribución Ligera en GitHub Pages (`db/db.go`, `.github/workflows/deploy.yml`)**: Compresión automática a `news_central.db.gz` (~12.5 MB sin Git LFS), autorrestauración local al iniciar y descompresión directa en navegador mediante `DecompressionStream('gzip')`.
- **Cron Automático en Linux (`start-services.sh`, `main.go`)**: Validador idempotente `--check-today` / `daily_runs` (`@reboot` y `5 * * * *` con `flock`) más `sync_db_git` para confirmar (`commit --only`) y subir (`push`) los snapshots `.db.gz` y `semantic_map.html`.

## 🔜 Próximos Pasos

- **Uso de proxies o Tor en SearXNG/Crawler**: Rotación de IP o salida `socks5h://localhost:9050` para reducir bloqueos de portales con anti-bot estricto.
- **Scrapers / Selectores Específicos**: Refinar reglas para hemerotecas estructuradas complejas.
- **Análisis de Sentimiento y Polaridad**: Añadir puntuación de tono por noticia/concepto y colorear el grafo según polaridad.
- **Exportación de Reportes**: Descargar subgrafos filtrados o resúmenes diarios en JSON/Markdown/PDF (con resúmenes opcionales por LLM local).
