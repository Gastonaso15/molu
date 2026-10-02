---
description: SDD · Implementa UNA sola tarea con TDD estricto (uso: /sdd-implement 001-nombre-spec T1)
agent: build
---
Implementa ÚNICAMENTE la tarea $2 de specs/$1/tasks.md, siguiendo rigurosamente specs/$1/plan.md, docs/constitution.md y la skill sdd.

Flujo de trabajo obligatorio:
1. **Tests primero**: Escribe o actualiza los tests unitarios (`pkg/.../*_test.go`) para cubrir la tarea $2 y ejecuta `go test -v -race ./...` para verificar que fallan (Rojo).
2. **Implementación de código**: Escribe el código en Go necesario para satisfacer los tests.
3. **Verificación en verde**: Ejecuta `go test -v -race ./...` asegurando que todos los tests pasen sin data races.
4. **Marcar y Parar**: Marca la tarea $2 como realizada (`[x]`) en specs/$1/tasks.md e indica qué RFs han quedado cubiertos.

Después PÁRATE INMEDIATAMENTE. No continúes con la siguiente tarea sin la aprobación expresa del usuario.
