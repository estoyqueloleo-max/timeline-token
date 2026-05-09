# Timeline Token

Herramienta de descubrimiento recursivo de portales de noticias, extracción de titulares y análisis de similitud semántica mediante Hugot y SQLite.

## 🚀 Características

- **Descubrimiento Inteligente**: Utiliza SearXNG para localizar portales de noticias y detecta automáticamente fuentes RSS o enlaces relevantes.
- **Extracción Recursiva**: Rastrea tanto fuentes RSS como enlaces internos de noticias para construir una base de datos profunda.
- **Búsqueda Relajada y Expansión Global**: Soporta tiempos de espera dinámicos para evitar baneos (`--delay`) y rastrea portales de España, Alemania, India, China, Rusia, **Argentina, Sudamérica, África y Canadá**.
- **Detección de Hemerotecas Asistida (LLM)**: Identifica URLs de archivo automáticamente de diarios de noticias, con soporte para patrones basados en fecha (YYYY/MM/DD).
- **Mapa Semántico Interactivo con Filtros**: Grafo dirigido de fuerza (D3.js) con **filtros para 10 tipos de entidades**, filtro dinámico por **país de origen (región)**, palabra clave y profundidad de recursión.
- **Análisis de Entidades (NER + Regex)**: Extrae e identifica Personas, Lugares, Organizaciones y más usando modelos de IA y patrones avanzados.
- **Procesamiento Incremental (`--resume`)**: Salta portales y noticias ya analizados basándose en hashes y entidades existentes, optimizando el tiempo de ejecución.
- **Análisis Semántico**: Genera vectores de características (embeddings) para cada titular utilizando el modelo `all-MiniLM-L6-v2` acelerado con OpenVINO.

## ⚙️ Opciones de Línea de Comandos

El ejecutable soporta las siguientes opciones para configurar el comportamiento del crawling y análisis:

| Flags | Descripción | Por defecto |
|-------|-------------|-------------|
| `--mode` | Modo de ejecución: `live` (RSS), `historical` (Archivos) o `both`. | `live` |
| `--resume` | Omite el descubrimiento y análisis para datos ya existentes en la DB. | `false` |
| `--delay` | Retraso entre consultas de búsqueda (ej: `5s`, `10s`). | `1s` |
| `--serve` | Inicia un servidor HTTP local para visualizar el mapa semántico. | `false` |
| `--port` | Puerto para el servidor HTTP local. | `8080` |
| `--llm` | URL del endpoint de un LLM local (Ollama/vLLM) para detección de archivos. | `http://192.168.1.4:4001` |
| `--model` | Nombre del modelo LLM a utilizar. | `qwen3` |
| `--test-archive-url` | Prueba la detección de archivos de un portal específico sin ejecutar el pipeline. | `""` |

## 🌐 Visualización en Vivo
Puedes ver el resultado de la última búsqueda aquí:
👉 **[Mapa Semántico Interactivo](https://estoyqueloleo-max.github.io/timeline-token/)**

## 🧠 ¿Cómo funciona? El Núcleo Semántico

La herramienta utiliza un enfoque de tres niveles para entender la información:

### 1. Embeddings (Significado Latente)
Convertimos cada titular en un vector de 384 dimensiones. Esto permite al sistema agrupar noticias similares aunque no compartan palabras exactas.

### 2. Reconocimiento de Entidades y Filtrado por Región
Extraemos categorías de información y permitimos filtrar por el origen de la noticia:
- **Modelos IA (NER)**: Personas (PER), Lugares (LOC), Organizaciones (ORG), Miscelánea (MISC).
- **Patrones (Regex)**: Fechas, Dinero, Horas, Cantidades, Links y Correos.
- **Filtro Geográfico**: Permite aislar tendencias de países específicos (`es`, `ar`, `fr`, `de`, `in`, `cn`, `ca`, `latam`, `africa`, `global`).

### 3. Co-ocurrencia y Navegación
El mapa visualiza las conexiones entre estas entidades. Puedes hacer clic en los nodos para ver las noticias asociadas, navegar por el historial de clics y ver resaltado contextual de palabras clave.

## 🚦 Inicio Rápido

1. **Instalar dependencias**:
   ```bash
   go mod tidy
   ```

2. **Compilar**:
   ```bash
   go build -o news-tool .
   ```

3. **Ejecutar con Procesamiento Incremental**:
   ```bash
   ./news-tool --mode=both --resume --delay=5s
   ```

4. **Visualización Interactiva**:
   ```bash
   ./news-tool --serve --port=8080
   ```
   Visita: [http://localhost:8080/semantic_map.html](http://localhost:8080/semantic_map.html)

## 🗄️ Persistencia Histórica

La base de datos SQLite (`news_central.db`) gestiona:
- **`portals`**: Registro de fuentes de noticias (nombre, RSS, región, idioma, estado de archivo).
- **`news`**: Metadatos de cada noticia vinculada a su portal de origen.
- **`news_entities`**: Todas las entidades extraídas vinculadas a sus noticias.
- **`news_embeddings`**: Vectores para búsquedas de similitud.

## 📝 Planes Futuros

- Implementar análisis de sentimientos como vector visual secundario.
- Integración de resúmenes automáticos de clústeres mediante LLM local.
- Exportación de reportes PDF con el grafo de relaciones.
