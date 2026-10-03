---
description: SDD · Divide el plan en tareas pequeñas y verificables
agent: build
---
A partir de specs/$1/spec.md y specs/$1/plan.md, CREA Y GUARDA DIRECTAMENTE en disco el archivo `specs/$1/tasks.md` siguiendo el formato de la skill sdd:
- Tareas pequeñas y atómicas (máximo 20-30 min cada una), ordenadas por estricta dependencia lógica.
- Cada una indicando los RF que cubre y una condición "Hecho cuando:" objetivamente verificable mediante tests (`go test -v -race`).
- Usa checkboxes de Markdown (`- [ ] **T1. ...**`).
- Límite recomendado: máximo 8 a 10 tareas. Si se requieren más, propón dividir la funcionalidad en dos specs.

IMPORTANTE: Guarda el archivo `specs/$1/tasks.md` directamente en disco con tus herramientas de escritura de archivos. No le pidas al usuario que lo copie y pegue manualmente.
