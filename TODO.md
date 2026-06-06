# Roadmap & Próximos Pasos - Timeline Token

- [x] **Soporte Intel GPU en GLiNER2**: Modificar el repositorio `hugot-gliner2` (`pkg/gliner/pipeline.go`) para que acepte opciones de ejecución ONNX (`AppendExecutionProviderOpenVINO`) y pueda delegar la carga en gráficas/CPUs de Intel vía OpenVINO.


- **Uso de proxy's o Tor**: Para evitar el bloque do portales.
outgoing:
  proxies:
    all: socks5h://localhost:9050  
    http:
      - http://usuario:password@ip_proxy2:puerto
    https:
      - http://usuario:password@ip_proxy2:puerto

- **Mapa Semántico**: Alimentado por SQLite directamente (`sql.js`), interactivo, con zoom y panel de noticias lateral.
- **Entidades Avanzadas**: Filtrado interactivo de 10 tipos de entidades (PER, LOC, ORG, DATE, MONEY, TIME, QUAN, URLL, EMAIL, MISC) con colores específicos. Boton para seleccionar/deseleccionar todos. Quizas quitar el negro para PER que serian los mas interesantes.
- **Procesamiento Incremental**: Uso de `--resume` para evitar re-analizar noticias y portales ya procesados.
- **Detección de Hemerotecas**: Lógica base con LLM, normalización de URLs y sistemas de fallback por reglas.
- **Poder lanzar busquedas**: En hemerotecas sobre palabras clave para aumentar el "historial" de la misma.

- **Filtro por Fecha**: No solo filtrar el desde, sino tambien el hasta. Usar fecha de escaneo, "mas real" y fecha extraida de la publicacion.
- **Filtro por Portal**: Selector en la UI para aislar noticias de un medio específico.
- **Relaciones Inter-Portal**: Visualizar cómo la misma noticia se trata en distintos medios.
- **Opticion para exponer**: Version reducida de la bd para poderla subir a una web (si se necesita terminar de precalcular cosas, adelante) para que sea ligera sin info que no es necesaria en una primera visualizacion.
- **Exportar Reporte**: Botón para descargar el grafo actual o la lista de noticias filtradas.

- **Scrapers Específicos**: Refinar la extracción para sitios complejos con hemerotecas estructuradas (ej: El Mundo).
- **Análisis de Sentimiento**: Añadir un código de colores al mapa para el tono de la noticia.
- **Detección de Tendencias (Burst Detection)**: Algoritmo para identificar conceptos con crecimiento explosivo de frecuencia.





Me gustaria subir esta carpeta (que esta dentro de otro repo) al siguiente repo...

echo "# timeline-token" >> README.md
git init
git add README.md
git commit -m "first commit"
git branch -M main
git remote add origin git@github.com-estoyqueloleo:estoyqueloleo-max/timeline-token.git
git push -u origin main

Me gustaria añadir en github un workflow para que el resultado (que dejare actualizado en la carpeta) de la bd y el html para visualizarlo se despligue en una github page del repo ... y se actualice el README para hacer referncia a ella como ejemplo resultante de la busqueda...

La bd podria llegar a pesar bastante por lo que (mientras no la optimicemos) lo mismo es intersante usar git lfs...





