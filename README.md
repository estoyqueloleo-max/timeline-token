# Timeline Token

Recursive news discovery tool, headline extraction, and semantic similarity analysis using Hugot, OpenVINO, and SQLite.

## 🚀 Features

- **Intelligent Discovery**: Uses SearXNG to locate news portals and automatically detects RSS feeds or relevant internal links.
- **Recursive Extraction**: Crawls both RSS feeds and internal news links to build a deep, structured database.
- **Relaxed Search & Global Expansion**: Supports dynamic timeouts to prevent bans (`--delay`) and tracks portals from Spain, Germany, India, China, Russia, **Argentina, South America, Africa, and Canada**.
- **LLM-Assisted Archive Detection**: Automatically identifies archive URLs from news outlets, with support for date-based patterns (YYYY/MM/DD).
- **Interactive Semantic Map**: D3.js force-directed graph with **filters for 10 entity types**, dynamic filtering by **country of origin (region)**, keywords, and recursion depth.
- **Entity Analysis (NER + Regex)**: Extracts and identifies Persons, Locations, Organizations, and more using local AI models and advanced patterns.
- **Incremental Processing (`--resume`)**: Skips already analyzed portals and news items based on hashes and existing entities, optimizing execution time.
- **Semantic Analysis**: Generates feature vectors (embeddings) for each headline using the `all-MiniLM-L6-v2` model accelerated with OpenVINO.

## ⚙️ Command Line Options

The executable supports the following options to configure crawling and analysis behavior:

| Flags | Description | Default |
|-------|-------------|---------|
| `--mode` | Execution mode: `live` (RSS), `historical` (Archives) or `both`. | `live` |
| `--resume` | Skips discovery and analysis for existing data in the DB. | `false` |
| `--delay` | Delay between search queries (e.g., `5s`, `10s`). | `1s` |
| `--serve` | Starts a local HTTP server to visualize the semantic map. | `false` |
| `--port` | Port for the local HTTP server. | `8080` |
| `--llm` | URL of a local LLM endpoint (Ollama/vLLM) for archive detection. | `http://192.168.1.4:4001` |
| `--model` | LLM model name to use. | `qwen3` |
| `--test-archive-url` | Tests archive detection for a specific portal without running the pipeline. | `""` |

## 🌐 Live Visualization

You can view the results of the latest crawl here:
👉 **[Interactive Semantic Map](https://estoyqueloleo-max.github.io/timeline-token/index.html)**

## 🧠 How it works: The Semantic Core

The tool uses a three-level approach to understand information:

### 1. Embeddings (Latent Meaning)
We convert each headline into a 384-dimensional vector. This allows the system to group similar news items even if they don't share exact keywords.

### 2. Entity Recognition & Region Filtering
We extract categories of information and allow filtering by news origin:
- **AI Models (NER)**: Persons (PER), Locations (LOC), Organizations (ORG), Miscellaneous (MISC).
- **Patterns (Regex)**: Dates, Money, Time, Quantities, Links, and Emails.
- **Geographic Filter**: Isolate trends from specific countries (`es`, `ar`, `fr`, `de`, `in`, `cn`, `ca`, `latam`, `africa`, `global`).

### 3. Co-occurrence & Navigation
The map visualizes connections between these entities. You can click on nodes to see associated news, navigate through click history, and see contextual keyword highlighting.

## 🚦 Quick Start

1. **Install dependencies**:
   ```bash
   go mod tidy
   ```

2. **Compile**:
   ```bash
   go build -o news-tool .
   ```

3. **Run with Incremental Processing**:
   ```bash
   ./news-tool --mode=both --resume --delay=5s
   ```

4. **Interactive Visualization**:
   ```bash
   ./news-tool --serve --port=8080
   ```
   Visit: [http://localhost:8080/semantic_map.html](http://localhost:8080/semantic_map.html)

## 🗄️ Historical Persistence

The SQLite database (`news_central.db`) manages:
- **`portals`**: Registry of news sources (name, RSS, region, language, archive status).
- **`news`**: Metadata for each news item linked to its source portal.
- **`news_entities`**: All extracted entities linked to their news items.
- **`news_embeddings`**: Vectors for similarity searches.

## 📝 Future Plans

- Implement sentiment analysis as a secondary visual vector.
- Automated cluster summarization integration using local LLM.
- PDF report export with relationship graphs.
