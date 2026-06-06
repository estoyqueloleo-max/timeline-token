# Variables de entorno por defecto para soporte GPU (OpenVINO)
# Apunta al runtime de ONNX que tiene compilado el soporte para Intel OpenVINO
ONNXRUNTIME_LIB_PATH ?= /home/jose/openvino/openvino/lib/python3.13/site-packages/onnxruntime/capi/libonnxruntime.so.1.25.1

.PHONY: all build run serve clean

all: build

build:
	@echo "🔨 Compilando timeline-token..."
	go build -tags ORT -o timeline-token .

run: build
	@echo "🚀 Ejecutando pipeline principal con soporte GPU..."
	ONNXRUNTIME_LIB_PATH=$(ONNXRUNTIME_LIB_PATH) ./timeline-token

serve: build
	@echo "🌐 Ejecutando pipeline y regenerando HTML (Modo Serve)..."
	ONNXRUNTIME_LIB_PATH=$(ONNXRUNTIME_LIB_PATH) ./timeline-token --serve

analyze: build
	@echo "🧠 Saltando web scraping... Ejecutando solo Análisis Semántico (GLiNER)..."
	ONNXRUNTIME_LIB_PATH=$(ONNXRUNTIME_LIB_PATH) ./timeline-token --analyze-only

clean:
	@echo "🧹 Limpiando binarios..."
	rm -f timeline-token
