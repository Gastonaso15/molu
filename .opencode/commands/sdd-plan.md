---
description: SDD · Genera el plan técnico de una spec aprobada
agent: build
---
Lee docs/constitution.md, AGENTS.md y specs/$1/spec.md. Usa la skill sdd.
NO escribas código de implementación en Go (no toques `pkg/` ni `cmd/`).

CREA Y GUARDA DIRECTAMENTE en disco el archivo `specs/$1/plan.md` incluyendo:
- Archivos en `pkg/` o `cmd/` que se crean o modifican y la responsabilidad de cada uno.
- Modelos y structs Go, interfaces desacopladas para xolu client y MCP transport (mockeables).
- Mapeo explícito a la taxonomía tipada (§2.4): prefijo `XOLU-MOLU-FRONT-*` para errores propios y preservación verbatim de `XOLU-*`.
- Interfaz RPI (`Admit` y `Score`) si afecta al descubrimiento o visibilidad de herramientas.
- Diagrama de secuencia Mermaid en formato de texto.
- Decisiones técnicas justificadas (y alternativas descartadas).
- Estrategia de tests unitarios y de integración con `go test -v -race ./...`.

IMPORTANTE: Guarda el archivo `specs/$1/plan.md` directamente en disco con tus herramientas de escritura de archivos. No le pidas al usuario que lo copie y pegue manualmente.
Todo debe respetar la constitución (cero menciones a "Seam" en código/defaults) y cubrir todos los RF.
Marca explícitamente qué RF cubre cada parte.
Si la spec no está aprobada o tiene dudas abiertas marcadas con [NECESITA ACLARACIÓN], para y avísame antes de continuar.
