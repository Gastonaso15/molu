---
description: SDD · Valida la spec RF por RF mediante tests automáticos y de integración
agent: build
---
Recorre specs/$1/spec.md requisito por requisito (RF por RF).

Para cada RF:
1. Indica qué archivo y función de test en Go lo cubre (ej.: `pkg/exec/probe_test.go:TestProbeWaitReady` o test dedicado de integración de taxonomía).
2. Muestra el resultado de ejecutar la suite de pruebas con `go test -v -race ./...`.
3. Para herramientas MCP expuestas o consumidas del Hub, verifica:
   - Que los primitivos no lleven namespace y las funciones de dominio lleven `namespace.Nombre`.
   - Que los rechazos de FSM o esquemas retornen los códigos tipados exactos (`INVALID_TRANSITION`, `CONTRACT_VIOLATION`, etc.).
4. Si la spec incluía diagramas de secuencia, verifica que existan los archivos `.mmd` y `.pdf`.

Si algún RF no está cubierto o algún test falla, dilo claramente. NO intentes arreglar código todavía.
Comprueba todos los criterios de finalización de specs/$1/spec.md y emite el veredicto final:
¿Está la especificación 100% cumplida y lista para cerrarse?
