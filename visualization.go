package main

import (
	"fmt"
	"os"
)

func GenerateVisualization() {
	fmt.Println("Step 5: Generating Interactive Semantic Network HTML with Advanced Filters...")

	htmlContent := `<!DOCTYPE html>
<html lang="es">
<head>
    <meta charset="utf-8">
    <title>Timeline Token - Semantic Map</title>
    <style>
        body, html {
            margin: 0;
            padding: 0;
            width: 100vw;
            height: 100vh;
            overflow: hidden;
            font-family: 'Segoe UI', Tahoma, Geneva, Verdana, sans-serif;
            background-color: #121212;
            color: #e0e0e0;
        }
        #graph-container {
            width: 100%;
            height: 100%;
            position: absolute;
            top: 0; left: 0;
            z-index: 1;
        }
        #controls {
            position: absolute;
            top: 20px;
            left: 20px;
            z-index: 100;
            background: rgba(30, 30, 30, 0.9);
            padding: 20px;
            border-radius: 8px;
            border: 1px solid #444;
            width: 300px;
            box-shadow: 0 4px 15px rgba(0,0,0,0.5);
            transition: transform 0.3s ease, opacity 0.3s ease;
        }
        #controls.collapsed {
            transform: translateX(-340px);
            opacity: 0;
            pointer-events: none;
        }
        #toggle-controls {
            position: absolute;
            top: 20px;
            left: 20px;
            z-index: 110;
            background: #69b3a2;
            border: none;
            color: #121212;
            width: 40px;
            height: 40px;
            border-radius: 8px;
            cursor: pointer;
            display: flex;
            align-items: center;
            justify-content: center;
            font-size: 1.2rem;
            box-shadow: 0 2px 5px rgba(0,0,0,0.3);
        }

        .control-group { margin-bottom: 20px; }
        .control-group label { display: block; margin-bottom: 8px; font-weight: bold; color: #69b3a2; }
        input[type="range"], input[type="text"], input[type="number"] {
            width: 100%;
            background: #2a2a2a;
            border: 1px solid #444;
            color: #fff;
            padding: 8px;
            border-radius: 4px;
            box-sizing: border-box;
        }
        #date-display { font-size: 0.8rem; margin-top: 5px; color: #aaa; text-align: center; }
        
        #info-panel {
            position: absolute;
            top: 0;
            right: -400px;
            width: 400px;
            height: 100vh;
            background: #1e1e1e;
            box-shadow: -2px 0 10px rgba(0,0,0,0.5);
            z-index: 101;
            transition: right 0.3s ease;
            display: flex;
            flex-direction: column;
            box-sizing: border-box;
            border-left: 1px solid #333;
        }
        #info-panel.open { right: 0; }
        #info-header {
            padding: 15px 20px;
            background: #252526;
            border-bottom: 1px solid #333;
            display: flex;
            justify-content: space-between;
            align-items: center;
        }
        #header-left { display: flex; align-items: center; gap: 10px; }
        #info-header h2 { margin: 0; font-size: 1.2rem; color: #69b3a2; text-transform: uppercase; }
        
        .nav-btn { background: none; border: none; color: #aaa; font-size: 1.2rem; cursor: pointer; padding: 5px; border-radius: 4px; }
        .nav-btn:hover { color: #fff; background: #444; }
        .nav-btn:disabled { color: #444; cursor: default; background: none; }

        #info-content { padding: 20px; overflow-y: auto; flex-grow: 1; }
        
        .article-entry { margin-bottom: 15px; padding: 10px; background: #2a2a2a; border-radius: 6px; border-left: 3px solid #69b3a2; }
        .article-entry a { color: #fff; text-decoration: none; font-weight: bold; display: block; margin-bottom: 5px; line-height: 1.4; }
        .article-entry a:hover { color: #4da6ff; }
        .article-entry .meta { font-size: 0.8rem; color: #888; }
        
        /* Highlighting keywords within titles */
        .kw-main { color: #ffeb3b; background: rgba(255, 235, 59, 0.2); padding: 0 2px; border-radius: 2px; }
        .kw-neighbor { color: #69b3a2; background: rgba(105, 179, 162, 0.2); padding: 0 2px; border-radius: 2px; }

        .links line { stroke: #555; stroke-opacity: 0.3; transition: stroke 0.3s, stroke-opacity 0.3s; }
        .nodes circle { stroke: #222; stroke-width: 1.5px; cursor: pointer; transition: fill 0.3s, stroke 0.3s; }
        
        .nodes circle.dimmed { opacity: 0.2; }
        .links line.dimmed { opacity: 0.05; }
        .nodes circle.highlight-center { stroke: #ffeb3b !important; stroke-width: 4px !important; opacity: 1 !important; }
        .nodes circle.highlight-neighbor { stroke: #69b3a2 !important; stroke-width: 2px !important; opacity: 1 !important; fill: #4da6ff !important; }
        .links line.highlight-link { stroke: #ffeb3b !important; stroke-opacity: 1 !important; stroke-width: 3px !important; }

        text { font-family: sans-serif; font-size: 12px; fill: #aaa; pointer-events: none; transition: opacity 0.3s; }
        text.dimmed { opacity: 0.1; }
        text.highlight-text { fill: #fff; font-weight: bold; opacity: 1 !important; font-size: 14px; }
        
        .link-label { font-size: 10px; fill: #888; pointer-events: none; text-anchor: middle; transition: opacity 0.3s; }
        .link-label.dimmed { opacity: 0.1; }
        .link-label.highlight-text { fill: #fff; opacity: 1 !important; font-size: 12px; font-weight: bold; }
        
        /* Node Colors by Type (Extended) */
        .node-per { fill: #ffeb3b !important; } /* Yellow - Persons */
        .node-loc { fill: #4da6ff !important; } /* Blue - Places */
        .node-org { fill: #4caf50 !important; } /* Green - Organizations */
        .node-misc { fill: #ab47bc !important; } /* Purple - Misc */
        .node-date { fill: #ff7043 !important; } /* Orange - Dates */
        .node-money { fill: #ec407a !important; } /* Pink - Money */
        .node-time { fill: #8d6e63 !important; } /* Brown - Time */
        .node-quan { fill: #78909c !important; } /* Slate - Quantity */
        .node-urll { fill: #d4e157 !important; } /* Lime - URLs */
        .node-email { fill: #00bcd4 !important; } /* Cyan - Email */
        .node-default { fill: #69b3a2 !important; } /* Default */

        button#apply-btn {
            width: 100%;
            padding: 10px;
            background: #69b3a2;
            border: none;
            color: #121212;
            font-weight: bold;
            border-radius: 4px;
            cursor: pointer;
            margin-top: 10px;
        }
        button#apply-btn:hover { background: #569b8b; }

        /* Range Slider CSS */
        .range-slider-wrapper {
            position: relative;
            width: 100%;
            height: 40px;
            margin-top: 10px;
        }
        .slider-control-container {
            position: relative;
            height: 6px;
            width: 100%;
            background: #444;
            border-radius: 3px;
        }
        .slider-track {
            position: absolute;
            height: 100%;
            background: #69b3a2;
            border-radius: 3px;
            z-index: 1;
        }
        .range-input-container {
            position: relative;
        }
        .range-input-container input {
            position: absolute;
            width: 100%;
            top: -6px;
            background: none;
            pointer-events: none;
            -webkit-appearance: none;
            -moz-appearance: none;
        }
        input[type="range"]::-webkit-slider-thumb {
            height: 18px;
            width: 18px;
            border-radius: 50%;
            background: #69b3a2;
            pointer-events: auto;
            cursor: pointer;
            box-shadow: 0 0 2px 0 #000;
            -webkit-appearance: none;
            border: 2px solid #1e1e1e;
        }
        input[type="range"]::-moz-range-thumb {
            height: 18px;
            width: 18px;
            border: none;
            border-radius: 50%;
            background: #69b3a2;
            pointer-events: auto;
            cursor: pointer;
            box-shadow: 0 0 2px 0 #000;
            border: 2px solid #1e1e1e;
        }

        /* Collapsible Sections */
        .collapsible-header {
            cursor: pointer;
            display: flex;
            justify-content: space-between;
            align-items: center;
            background: #252526;
            padding: 8px 12px;
            border-radius: 4px;
            margin-bottom: 5px;
            border-left: 3px solid #69b3a2;
        }
        .collapsible-header:hover { background: #333; }
        .collapsible-header .toggle-icon { font-size: 0.8rem; transition: transform 0.3s; }
        .collapsible-header.collapsed .toggle-icon { transform: rotate(-90deg); }
        .collapsible-content { overflow: hidden; transition: max-height 0.3s ease-out; }
        .collapsible-content.collapsed { max-height: 0 !important; }

        .filter-actions {
            display: flex;
            gap: 10px;
            margin-bottom: 8px;
            font-size: 0.75rem;
        }
        .filter-actions span {
            color: #69b3a2;
            cursor: pointer;
            text-decoration: underline;
        }
        .filter-actions span:hover { color: #fff; }
    </style>
    <script src="https://cdnjs.cloudflare.com/ajax/libs/sql.js/1.8.0/sql-wasm.js"></script>
    <script src="https://d3js.org/d3.v7.min.js"></script>
</head>
<body>

<div id="loading">Cargando datos...</div>

<button id="toggle-controls" title="Mostrar/Ocultar Filtros">⚙️</button>

<div id="controls">
    <div class="control-group">
        <label>Filtro Temporal (Rango)</label>
        <div class="range-slider-wrapper">
            <div class="slider-control-container">
                <div id="slider-track" class="slider-track"></div>
            </div>
            <div class="range-input-container">
                <input type="range" id="date-slider-min" min="0" max="100" value="0">
                <input type="range" id="date-slider-max" min="0" max="100" value="100">
            </div>
        </div>
        <div id="date-display" style="margin-top: 15px; font-size: 0.8rem; color: #aaa; text-align: center;">...</div>
        <div style="display: flex; flex-direction: column; gap: 5px; margin-top: 10px;">
            <div style="font-size: 0.7rem; color: #888;">Inicio:</div>
            <input type="datetime-local" id="date-input-min" style="font-size: 0.8rem; padding: 4px;">
            <div style="font-size: 0.7rem; color: #888;">Fin:</div>
            <input type="datetime-local" id="date-input-max" style="font-size: 0.8rem; padding: 4px;">
        </div>
    </div>
    <div class="control-group">
        <label>Buscar Palabra</label>
        <input type="text" id="search-input" placeholder="Ej: economía, guerra...">
    </div>
    <div class="control-group">
        <div class="collapsible-header" onclick="toggleSection('region-section')">
            <span>Filtro por Región</span>
            <span class="toggle-icon">▼</span>
        </div>
        <div id="region-section" class="collapsible-content" style="max-height: 500px;">
            <div class="filter-actions">
                <span onclick="selectAll('region-filters', true)">Todos</span>
                <span onclick="selectAll('region-filters', false)">Ninguno</span>
            </div>
            <div id="region-filters" style="display: grid; grid-template-columns: 1fr 1fr; gap: 5px; font-size: 0.8rem; margin-bottom: 10px;">
                <!-- Dinámicamente poblado -->
            </div>
        </div>
    </div>
    <div class="control-group">
        <div class="collapsible-header" onclick="toggleSection('relation-section')">
            <span>Filtro de Relaciones</span>
            <span class="toggle-icon">▼</span>
        </div>
        <div id="relation-section" class="collapsible-content" style="max-height: 500px;">
            <div class="filter-actions">
                <span onclick="selectAll('relation-filters', true)">Todos</span>
                <span onclick="selectAll('relation-filters', false)">Ninguno</span>
            </div>
            <div id="relation-filters" style="display: flex; flex-direction: column; gap: 5px; font-size: 0.8rem; height: 300px; overflow-y: auto;">
                <!-- Dinámicamente poblado -->
            </div>
        </div>
    </div>
    <button id="apply-btn">Aplicar Filtros</button>
</div>

<div id="graph-container">
    <svg width="100%" height="100%"></svg>
</div>

<div id="info-panel">
    <div id="info-header">
        <div id="header-left">
            <button id="back-btn" class="nav-btn" title="Volver atrás" disabled>⬅</button>
            <h2 id="node-title">Word</h2>
        </div>
        <button id="close-btn" class="nav-btn" title="Cerrar">&times;</button>
    </div>
    <div id="info-content"></div>
</div>

<script>
    const stopwords = new Set(["de","la","que","el","en","y","a","los","del","se","las","por","un","para","con","no","una","su","al","lo","como","más","pero","sus","le","the","of","and","to","in","for","is","on","that","by","this","with","i","you","it","not","or","be","are","from","at","as","your","all","have","new","more","an","was","we","will","home","can","us","about","if","page","my","has","search","free","sobre","este","ya","sus","muy","sin","sobre","ha","han","he","hemos","hay","eso","esta","esto"]);
    
    let db;
    let allNews = [];
    let allRelations = [];
    let wordTypes = {};
    let simulation, svg, container;
    let nodeSelection, linkSelection, textSelection, labelSelection;
    let globalLinks = [];
    let wordToArticlesMap = {};
    
    let clickHistory = [];
    let currentHighlightedNeighbors = new Set();

    const MIN_COUNT = 3;
    const MIN_CO_OCCURRENCE = 2;

    async function init() {
        try {
            const sqlPromise = initSqlJs({ locateFile: file => 'https://cdnjs.cloudflare.com/ajax/libs/sql.js/1.8.0/' + file });
            const dataPromise = fetch("news_central.db").then(res => res.arrayBuffer());
            const [SQL, buf] = await Promise.all([sqlPromise, dataPromise]);
            db = new SQL.Database(new Uint8Array(buf));

            const stmt = db.prepare("SELECT id, title, url, COALESCE(published_date, created_at) as published_date, COALESCE(region, 'global') as region FROM news WHERE title IS NOT NULL ORDER BY published_date DESC");
            while(stmt.step()) { allNews.push(stmt.getAsObject()); }
            stmt.free();
            
            const entStmt = db.prepare("SELECT word, type, COUNT(*) as c FROM news_entities GROUP BY word, type ORDER BY c DESC");
            while(entStmt.step()) {
                const row = entStmt.getAsObject();
                if (!wordTypes[row.word]) wordTypes[row.word] = row.type;
            }
            entStmt.free();
            
            // Cargar relaciones
            try {
                const relStmt = db.prepare("SELECT news_id, head, tail, label, score FROM news_relations WHERE score > 0.1");
                while(relStmt.step()) { allRelations.push(relStmt.getAsObject()); }
                relStmt.free();
            } catch(e) {
                console.log("Tabla news_relations no encontrada, asegurate de ejecutar el pipeline antes.");
            }
            
            // Populate relation filters
            const relationLabels = [...new Set(allRelations.map(r => r.label).filter(l => l))].sort();
            const relContainer = document.getElementById("relation-filters");
            relationLabels.forEach(lbl => {
                const label = document.createElement("label");
                label.style.fontWeight = "normal";
                label.style.color = "#aaa";
                const checkbox = document.createElement("input");
                checkbox.type = "checkbox";
                checkbox.value = lbl;
                checkbox.checked = true;
                checkbox.addEventListener("change", updateGraph);
                label.appendChild(checkbox);
                label.appendChild(document.createTextNode(" " + lbl));
                relContainer.appendChild(label);
            });

            // Populate region filters
            const regions = [...new Set(allNews.map(n => n.region).filter(r => r))].sort();
            const regionContainer = document.getElementById("region-filters");
            regions.forEach(reg => {
                const label = document.createElement("label");
                label.style.fontWeight = "normal";
                label.style.color = "#aaa";
                label.innerHTML = '<input type="checkbox" value="' + reg + '"> ' + reg;
                regionContainer.appendChild(label);
            });

            if (allNews.length > 0) {
                const dates = allNews.map(n => new Date(n.published_date).getTime()).filter(d => !isNaN(d));
                const minDateVal = Math.min(...dates);
                const maxDateVal = Math.max(...dates);
                
                const sliderMin = document.getElementById("date-slider-min");
                const sliderMax = document.getElementById("date-slider-max");
                
                // Default to last 7 days or min if less than 7 days of data
                const last7d = maxDateVal - (7 * 24 * 60 * 60 * 1000);
                const initialStart = Math.max(minDateVal, last7d);

                sliderMin.min = minDateVal; sliderMin.max = maxDateVal; sliderMin.value = initialStart;
                sliderMax.min = minDateVal; sliderMax.max = maxDateVal; sliderMax.value = maxDateVal;
                
                updateDateDisplay(initialStart, maxDateVal);
            }

            svg = d3.select("svg");
            
            // Definir flecha (marker-end)
            svg.append("defs").append("marker")
                .attr("id", "arrowhead")
                .attr("viewBox", "-0 -5 10 10")
                .attr("refX", 25)
                .attr("refY", 0)
                .attr("orient", "auto")
                .attr("markerWidth", 6)
                .attr("markerHeight", 6)
                .attr("xoverflow", "visible")
                .append("svg:path")
                .attr("d", "M 0,-5 L 10 ,0 L 0,5")
                .attr("fill", "#69b3a2")
                .style("stroke","none");

            container = svg.append("g");
            
            const zoom = d3.zoom().scaleExtent([0.1, 4]).on("zoom", (e) => container.attr("transform", e.transform));
            svg.call(zoom);

            svg.on("click", (event) => {
                if (event.target.tagName === "svg") {
                    resetHighlight();
                }
            });

            document.getElementById("toggle-controls").addEventListener("click", () => {
                document.getElementById("controls").classList.toggle("collapsed");
            });

            document.getElementById("back-btn").addEventListener("click", goBack);

            document.getElementById("loading").style.display = "none";
            updateGraph();

        } catch (err) {
            document.getElementById("loading").innerText = "Error: " + err;
            console.error(err);
        }
    }

    function updateDateDisplay(min, max) {
        const start = new Date(min).toLocaleDateString();
        const end = new Date(max).toLocaleDateString();
        document.getElementById("date-display").innerText = "Rango: " + start + " - " + end;
        
        // Update slider track
        const sliderMin = document.getElementById("date-slider-min");
        const sliderMax = document.getElementById("date-slider-max");
        const track = document.getElementById("slider-track");
        
        const minVal = parseInt(sliderMin.min);
        const maxVal = parseInt(sliderMin.max);
        const range = maxVal - minVal;
        
        if (range > 0) {
            const leftPerc = ((parseInt(sliderMin.value) - minVal) / range) * 100;
            const rightPerc = ((parseInt(sliderMax.value) - minVal) / range) * 100;
            track.style.left = leftPerc + "%";
            track.style.width = (rightPerc - leftPerc) + "%";
        }

        // Update manual inputs
        document.getElementById("date-input-min").value = toIsoStringWithOffset(new Date(min));
        document.getElementById("date-input-max").value = toIsoStringWithOffset(new Date(max));
    }

    function toIsoStringWithOffset(date) {
        const tzo = -date.getTimezoneOffset();
        const dif = tzo >= 0 ? '+' : '-';
        const pad = (num) => (num < 10 ? '0' : '') + num;
        return date.getFullYear() +
            '-' + pad(date.getMonth() + 1) +
            '-' + pad(date.getDate()) +
            'T' + pad(date.getHours()) +
            ':' + pad(date.getMinutes());
    }

    const MIN_GAP_MS = 60 * 60 * 1000; // 1 hour gap

    document.getElementById("date-slider-min").addEventListener("input", (e) => {
        let minVal = parseInt(e.target.value);
        let maxVal = parseInt(document.getElementById("date-slider-max").value);
        
        if (minVal > maxVal - MIN_GAP_MS) {
            minVal = maxVal - MIN_GAP_MS;
            e.target.value = minVal;
        }
        updateDateDisplay(minVal, maxVal);
    });
    
    document.getElementById("date-slider-max").addEventListener("input", (e) => {
        let minVal = parseInt(document.getElementById("date-slider-min").value);
        let maxVal = parseInt(e.target.value);
        
        if (maxVal < minVal + MIN_GAP_MS) {
            maxVal = minVal + MIN_GAP_MS;
            e.target.value = maxVal;
        }
        updateDateDisplay(minVal, maxVal);
    });

    document.getElementById("date-input-min").addEventListener("change", (e) => {
        const newDate = new Date(e.target.value).getTime();
        if (isNaN(newDate)) return;
        
        const sliderMin = document.getElementById("date-slider-min");
        const sliderMax = document.getElementById("date-slider-max");
        const currentMax = parseInt(sliderMax.value);
        const totalMin = parseInt(sliderMin.min);
        
        let finalDate = Math.max(totalMin, Math.min(newDate, currentMax - MIN_GAP_MS));
        sliderMin.value = finalDate;
        updateDateDisplay(finalDate, currentMax);
    });

    document.getElementById("date-input-max").addEventListener("change", (e) => {
        const newDate = new Date(e.target.value).getTime();
        if (isNaN(newDate)) return;

        const sliderMin = document.getElementById("date-slider-min");
        const sliderMax = document.getElementById("date-slider-max");
        const currentMin = parseInt(sliderMin.value);
        const totalMax = parseInt(sliderMax.max);
        
        let finalDate = Math.min(totalMax, Math.max(newDate, currentMin + MIN_GAP_MS));
        sliderMax.value = finalDate;
        updateDateDisplay(currentMin, finalDate);
    });

    document.getElementById("apply-btn").addEventListener("click", updateGraph);

    function updateGraph() {
        const minDate = parseInt(document.getElementById("date-slider-min").value);
        const maxDate = parseInt(document.getElementById("date-slider-max").value);
        const searchText = document.getElementById("search-input").value.toLowerCase().trim();
        const selectedRegions = new Set(Array.from(document.querySelectorAll("#region-filters input:checked")).map(i => i.value));
        const selectedRelations = new Set(Array.from(document.querySelectorAll("#relation-filters input:checked")).map(i => i.value));
        const depthInput = document.getElementById("recursion-depth");
        const depth = depthInput ? parseInt(depthInput.value) : 1;

        const filteredNews = allNews.filter(n => {
            const time = new Date(n.published_date).getTime();
            const dateMatch = time >= minDate && time <= maxDate;
            const regionMatch = selectedRegions.has(n.region);
            return dateMatch && regionMatch;
        });
        
        const filteredNewsMap = {};
        filteredNews.forEach(n => filteredNewsMap[n.id] = n);

        const wordCounts = {};
        const coOccurrences = {};
        wordToArticlesMap = {};

        // Extraer grafo de knowledge graph (relaciones)
        const relevantRelations = allRelations.filter(r => filteredNewsMap[r.news_id] && selectedRelations.has(r.label));
        
        relevantRelations.forEach(r => {
            const w1 = r.head;
            const w2 = r.tail;
            const newsItem = filteredNewsMap[r.news_id];
            
            wordCounts[w1] = (wordCounts[w1] || 0) + 1;
            wordCounts[w2] = (wordCounts[w2] || 0) + 1;
            
            if(!wordToArticlesMap[w1]) wordToArticlesMap[w1] = [];
            if(!wordToArticlesMap[w1].find(a => a.url === newsItem.url)) {
                 wordToArticlesMap[w1].push({title: newsItem.title, url: newsItem.url, date: newsItem.published_date, region: newsItem.region});
            }
            if(!wordToArticlesMap[w2]) wordToArticlesMap[w2] = [];
            if(!wordToArticlesMap[w2].find(a => a.url === newsItem.url)) {
                 wordToArticlesMap[w2].push({title: newsItem.title, url: newsItem.url, date: newsItem.published_date, region: newsItem.region});
            }

            if(!coOccurrences[w1]) coOccurrences[w1] = {};
            coOccurrences[w1][w2] = { weight: (coOccurrences[w1][w2]?.weight || 0) + 1, label: r.label };
            if(!coOccurrences[w2]) coOccurrences[w2] = {};
        });

        let nodesToInclude = new Set();
        const initialNodes = Object.keys(wordCounts).filter(w => wordCounts[w] >= 1); // Bajado a 1 para mostrar el grafo

        if (searchText) {
            let currentLevel = initialNodes.filter(w => w.includes(searchText));
            currentLevel.forEach(n => nodesToInclude.add(n));
            
            for (let i = 0; i < depth; i++) {
                let neighbors = new Set();
                currentLevel.forEach(node => {
                    const adj = coOccurrences[node] || {};
                    Object.keys(adj).forEach(neighbor => {
                        if (initialNodes.includes(neighbor) && !nodesToInclude.has(neighbor)) {
                            neighbors.add(neighbor);
                        }
                    });
                    
                    // También buscar hacia atrás
                    Object.keys(coOccurrences).forEach(k => {
                        if(coOccurrences[k][node] && initialNodes.includes(k) && !nodesToInclude.has(k)) {
                            neighbors.add(k);
                        }
                    });
                });
                neighbors.forEach(n => nodesToInclude.add(n));
                currentLevel = Array.from(neighbors);
            }
        } else {
            // SHOW TOP 300 HUBS ONLY IF NO SEARCH TEXT
            const sortedHubs = Object.keys(wordCounts)
                .sort((a,b) => wordCounts[b] - wordCounts[a])
                .slice(0, 300);
                
            sortedHubs.forEach(n => nodesToInclude.add(n));
        }

        const nodes = Array.from(nodesToInclude)
            .map(w => ({
                id: w, 
                value: Math.min(25, wordCounts[w]), // Limitar el tamaño visual
                type: wordTypes[w] || 'default'
            }));

        globalLinks = [];
        const seenLinks = new Set();
        const finalNodeIds = new Set(nodes.map(n => n.id));

        nodes.forEach(n1 => {
            const w1 = n1.id;
            Object.keys(coOccurrences[w1] || {}).forEach(w2 => {
                if(finalNodeIds.has(w2)) {
                    const data = coOccurrences[w1][w2];
                    const weight = data.weight;
                    const idKey = [w1, w2, data.label].join("|");
                    if(!seenLinks.has(idKey)) {
                        globalLinks.push({source: w1, target: w2, value: weight, label: data.label});
                        seenLinks.add(idKey);
                    }
                }
            });
        });

        renderD3(nodes, globalLinks);
    }

    function renderD3(nodes, links) {
        container.selectAll("*").remove();
        if (simulation) simulation.stop();

        const width = window.innerWidth;
        const height = window.innerHeight;

        simulation = d3.forceSimulation(nodes)
            .force("link", d3.forceLink(links).id(d => d.id).distance(100))
            .force("charge", d3.forceManyBody().strength(-200))
            .force("center", d3.forceCenter(width / 2, height / 2))
            .force("collide", d3.forceCollide().radius(d => Math.sqrt(d.value) * 6 + 10).iterations(2));

        const linkGroup = container.append("g").attr("class", "links").selectAll("g").data(links).enter().append("g");
        
        linkSelection = linkGroup.append("line")
            .attr("stroke-width", d => Math.sqrt(d.value) * 1.5)
            .attr("marker-end", "url(#arrowhead)");
            
        labelSelection = linkGroup.append("text")
            .attr("class", "link-label")
            .attr("dy", -5)
            .text(d => d.label);

        nodeSelection = container.append("g")
            .attr("class", "nodes")
            .selectAll("g")
            .data(nodes)
            .enter().append("g");

        nodeSelection.append("circle")
            .attr("r", d => Math.max(10, Math.sqrt(d.value) * 6))
            .attr("class", d => "node-" + d.type.toLowerCase())
            .on("click", (event, d) => {
                event.stopPropagation();
                handleNodeClick(d);
            })
            .on("dblclick", (event, d) => {
                event.stopPropagation();
                document.getElementById("search-input").value = d.id;
                updateGraph();
            })
            .call(d3.drag().on("start", dragstarted).on("drag", dragged).on("end", dragended));

        textSelection = nodeSelection.append("text")
            .text(d => d.id)
            .attr("x", d => Math.sqrt(d.value) * 6 + 10)
            .attr("y", 5);

        simulation.on("tick", () => {
            linkSelection.attr("x1", d => d.source.x).attr("y1", d => d.source.y).attr("x2", d => d.target.x).attr("y2", d => d.target.y);
            
            // Colocar etiquetas en el centro de la línea
            labelSelection.attr("x", d => (d.source.x + d.target.x) / 2)
                          .attr("y", d => (d.source.y + d.target.y) / 2);
                          
            nodeSelection.attr("transform", d => "translate(" + d.x + "," + d.y + ")");
        });

        function dragstarted(event, d) { if (!event.active) simulation.alphaTarget(0.3).restart(); d.fx = d.x; d.fy = d.y; }
        function dragged(event, d) { d.fx = event.x; d.fy = event.y; }
        function dragended(event, d) { if (!event.active) simulation.alphaTarget(0); d.fx = null; d.fy = null; }
    }

    function handleNodeClick(nodeData) {
        if (clickHistory.length === 0 || clickHistory[clickHistory.length - 1].id !== nodeData.id) {
            clickHistory.push(nodeData);
            document.getElementById("back-btn").disabled = clickHistory.length <= 1;
        }
        
        applyHighlight(nodeData.id);
        openInfoPanel(nodeData.id, wordToArticlesMap[nodeData.id]);
    }

    function goBack() {
        if (clickHistory.length > 1) {
            clickHistory.pop();
            const prevNode = clickHistory[clickHistory.length - 1];
            applyHighlight(prevNode.id);
            openInfoPanel(prevNode.id, wordToArticlesMap[prevNode.id]);
            document.getElementById("back-btn").disabled = clickHistory.length <= 1;
        }
    }

    function applyHighlight(nodeId) {
        currentHighlightedNeighbors = new Set();
        currentHighlightedNeighbors.add(nodeId);

        linkSelection.classed("highlight-link", d => {
            const isConnected = d.source.id === nodeId || d.target.id === nodeId;
            if (isConnected) {
                currentHighlightedNeighbors.add(d.source.id);
                currentHighlightedNeighbors.add(d.target.id);
            }
            return isConnected;
        }).classed("dimmed", d => {
            return d.source.id !== nodeId && d.target.id !== nodeId;
        });

        nodeSelection.selectAll("circle")
            .classed("highlight-center", d => d.id === nodeId)
            .classed("highlight-neighbor", d => d.id !== nodeId && currentHighlightedNeighbors.has(d.id))
            .classed("dimmed", d => !currentHighlightedNeighbors.has(d.id));

        textSelection
            .classed("highlight-text", d => currentHighlightedNeighbors.has(d.id))
            .classed("dimmed", d => !currentHighlightedNeighbors.has(d.id));
            
        labelSelection
            .classed("highlight-text", d => d.source.id === nodeId || d.target.id === nodeId)
            .classed("dimmed", d => d.source.id !== nodeId && d.target.id !== nodeId);
    }

    function resetHighlight() {
        linkSelection.classed("highlight-link", false).classed("dimmed", false);
        nodeSelection.selectAll("circle").classed("highlight-center", false).classed("highlight-neighbor", false).classed("dimmed", false);
        textSelection.classed("highlight-text", false).classed("dimmed", false);
        labelSelection.classed("highlight-text", false).classed("dimmed", false);
        clickHistory = [];
        currentHighlightedNeighbors = new Set();
        document.getElementById("back-btn").disabled = true;
    }

    function highlightText(text, mainWord, neighborSet) {
        let highlighted = text;
        const escape = str => str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
        
        const mainRegex = new RegExp('(' + escape(mainWord) + ')', 'gi');
        highlighted = highlighted.replace(mainRegex, '<span class="kw-main">$1</span>');
        
        neighborSet.forEach(nb => {
            if (nb !== mainWord) {
                const nbRegex = new RegExp('(' + escape(nb) + ')', 'gi');
                highlighted = highlighted.replace(nbRegex, '<span class="kw-neighbor">$1</span>');
            }
        });
        
        return highlighted;
    }

    function openInfoPanel(word, articles) {
        const panel = document.getElementById("info-panel");
        const titleLabel = document.getElementById("node-title");
        const content = document.getElementById("info-content");
        titleLabel.innerText = word;
        content.innerHTML = "";
        
        if(articles) {
            const selectedRegions = new Set(Array.from(document.querySelectorAll("#region-filters input:checked")).map(i => i.value));
            const filteredArticles = articles.filter(art => selectedRegions.has(art.region));
            
            filteredArticles.sort((a,b) => new Date(b.date) - new Date(a.date));
            filteredArticles.forEach(art => {
                const div = document.createElement("div");
                div.className = "article-entry";
                
                const highlightedTitle = highlightText(art.title, word, currentHighlightedNeighbors);
                
                div.innerHTML = "<a href='" + art.url + "' target='_blank'>" + highlightedTitle + "</a><div class='meta'>" + new Date(art.date).toLocaleString() + " | " + art.region + "</div>";
                content.appendChild(div);
            });
        }
        panel.classList.add("open");
    }

    document.getElementById("close-btn").addEventListener("click", () => {
        document.getElementById("info-panel").classList.remove("open");
        resetHighlight();
    });
    
    function toggleSection(id) {
        const content = document.getElementById(id);
        const header = content.previousElementSibling;
        content.classList.toggle("collapsed");
        header.classList.toggle("collapsed");
    }

    function selectAll(containerId, bool) {
        const inputs = document.querySelectorAll("#" + containerId + " input");
        inputs.forEach(i => i.checked = bool);
    }
    
    init();
</script>
</body>
</html>`

	err := os.WriteFile("semantic_map.html", []byte(htmlContent), 0644)
	if err != nil {
		fmt.Printf("Error writing html: %v\n", err)
	} else {
		fmt.Println("Created advanced semantic_map.html with contextual highlighting. Use '--serve' to view it interactively.")
	}
}
