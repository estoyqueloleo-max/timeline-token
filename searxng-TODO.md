El problema es que estás intentando llamar a motores que **ya no existen físicamente** en el código fuente de las versiones modernas de SearXNG (como la 2025.12 que estás usando). SearXNG es un proyecto muy dinámico y eliminan archivos `.py` si el sitio web de origen cambia o si el motor deja de recibir mantenimiento.

Si ves un `FileNotFoundError` apuntando a un archivo `.py`, no hay configuración que valga: **ese motor ha sido borrado del programa.**

Aquí tienes cómo limpiar tu `settings.yml` para que deje de dar errores y qué alternativas tienes para esa búsqueda histórica que necesitas.

---

### 1. ¿Qué motores debes borrar? (Ya no existen)

Basado en tus logs, estos archivos han desaparecido del core de SearXNG, por lo que debes eliminarlos o comentarlos en tu `settings.yml`:

* **`chronicling_america`**: Eliminado (puedes usar el de la `loc` que es similar).
* **`trove`**: Eliminado del repositorio oficial.
* **`internet_archive`**: El nombre correcto en versiones nuevas suele ser `archive_org` o simplemente ya no está disponible como motor directo (se usa a través de otros).
* **`europeana`**: Eliminado.
* **`ahmia` / `torch**`: Dan error porque requieren una conexión a **Tor** activa. Si no tienes Tor corriendo, coméntalos.

---

### 2. Configuración "Limpia" y Optimizada (Noticias e Historia)

He ajustado tu `settings.yml` manteniendo solo lo que **sí funciona** en 2026 y corrigiendo los nombres internos. Copia y reemplaza:

```yaml
use_default_settings: True

server:
  port: 8080
  bind_address: "0.0.0.0"
  secret_key: "una_clave_para_tu_notebook" 
  limiter: false 

search:
  safe_search: 1
  autocomplete: "google"
  formats:
    - html
    - json

engines:
  # --- NOTICIAS (Funcionan 100%) ---
  - name: google news
    engine: google_news
  
  - name: bing news
    engine: bing_news
    categories: news

  - name: reuters
    engine: reuters
    categories: news

  # --- HISTORIA Y ARCHIVOS (Los supervivientes) ---
  - name: library of congress
    engine: loc
    shortcut: loc
    categories: [news, history]

  - name: wikipedia
    engine: wikipedia
    shortcut: w

  - name: wikidata
    engine: wikidata
    shortcut: wd
    categories: [general, history]

  # --- MOTORES GENERALES PARA RELLENAR HUECOS ---
  - name: google
    engine: google
    shortcut: g
  
  - name: duckduckgo
    engine: duckduckgo
    shortcut: ddg

  - name: arxiv
    engine: arxiv
    shortcut: ar
    categories: [science, history]

ui:
  default_locale: "es"

outputs:
  - html
  - json

```

---

### 3. El "Truco" para buscar en la Hemeroteca sin motores específicos

Como muchos motores de archivos históricos han desaparecido de SearXNG, la mejor estrategia ahora es usar los motores generales (**Google** o **Bing**) con comandos avanzados de búsqueda (**Dorks**).

Si quieres que tu script de Python busque noticias históricas, puedes pedirle que busque en sitios específicos usando el motor de Google. Por ejemplo, en tu búsqueda podrías añadir:

* `site:archive.org "término de búsqueda"`
* `site:chroniclingamerica.loc.gov "término de búsqueda"`
* `site:hemerotecadigital.bne.es "término de búsqueda"` (Para España)

### 4. ¿Por qué sigue fallando Ahmia/Torch?

Aunque estos motores sí existen en el código, SearXNG los desactiva (`set engine to inactive!`) al arrancar si no detecta una red **SOCKS5** conectada a Tor.

* **Si no necesitas la Dark Web:** Simplemente ignora el error o quítalos.
* **Si los necesitas:** Debes instalar el paquete `tor` en tu sistema y añadir esto a tu `settings.yml`:
```yaml
outgoing:
  proxies:
    all: socks5h://localhost:9050

```



### Resumen de acción:

1. **Edita** tu `settings.yml` y quita `trove`, `europeana`, `chronicling_america` e `internet_archive`.
2. **Reinicia** el contenedor o el proceso.
3. Verás que los errores de "FileNotFound" desaparecen y SearXNG arrancará mucho más rápido.

¿Quieres que te ayude a crear una pequeña función en Python para automatizar las búsquedas en sitios históricos específicos usando el formato JSON que ya tienes habilitado?