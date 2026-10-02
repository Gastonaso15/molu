# AGENTS.md — Guía para Agentes en Molu & Hub

Bienvenido al repositorio **Molu**. Este proyecto desarrolla la interfaz de agentes de IA para el sustrato operacional xolu, compuesta por dos servicios centrales en Go:
1. **`molu` (frontal MCP)**: Expone herramientas MCP (primitivos genéricos y funciones de dominio descubiertas).
2. **`Hub de Seam` (Registry MCP)**: Catálogo desacoplado que registra funciones de publicadores y filtra la superficie ofrecida al agente.

---

## Comandos Principales

- **Tests unitarios y de concurrencia**: `go test -v -race ./...` (o `make test`)
- **Compilación del binario**: `make build` (genera `bin/molu`)
- **Cobertura de código**: `make coverage`
- **Ejecución local**: `make run-molu`

---

## Mandatos Normativos Innegociables

1. **Taxonomía tipada de resultados (§2.4)**:
   Seis códigos tipados obligatorios que **nunca se colapsan** ni se emiten como errores genéricos:
   - `NOT_OFFERED`: Función o entidad fuera de alcance para este tenant/rol.
   - `NOT_FOUND`: Objeto direccionado inexistente para este tenant.
   - `EMPTY`: Consulta válida que no arrojó resultados (prohibido rellenar con *padding*).
   - `INVALID_TRANSITION`: Transición FSM rechazada por xolu (preservar diagnóstico de guarda verbatim).
   - `CONTRACT_VIOLATION`: Incumplimiento del schema JSON de entrada (detalla campos infractores).
   - `SUBSTRATE_UNAVAILABLE`: xolu no disponible (incluye último contacto exitoso y próximo reintento).
   *Cada código debe contar con una prueba de integración dedicada.*

2. **Los dos ejes de control (§2.2)**:
   - **Eje 1 (Qué se ofrece)**: Resuelto por la interfaz RPI (`Admit` y `Score`) en el Hub/molu. Proveedores V1: `admit-all` y `static-context-rules`.
   - **Eje 2 (Qué puede ejecutarse)**: Resuelto por las FSMs de xolu invocadas vía el primitivo genérico `walk`.

3. **Convención estricta de nombres (§2.6)**:
   - **Primitivos genéricos contra xolu**: NUNCA llevan namespace (`walk`, `find`, `get`).
   - **Funciones de dominio publicadas al Hub**: SIEMPRE llevan `namespace.Nombre` (e.g. `work_orders.CreateOrder`).

4. **Neutralidad de dominio**:
   - Ni molu ni el Hub deben contener en su código fuente, constantes o valores por defecto la palabra "Seam". Toda lógica de dominio específica pertenece exclusivamente al publicador externo.

5. **Entregables de documentación obligatorios (§2.3.1)**:
   - Despliegue (orden tolerado, reintentos con backoff, transportes stdio/streamable-http).
   - Configuración (lista exhaustiva de variables de entorno `MOLU_FRONT_*` con valores por defecto y justificación operativa).
   - Runbook operativo (diagnóstico de errores de taxonomía y pautas ante `SUBSTRATE_UNAVAILABLE`).
   - Diagramas de secuencia propios en `.mmd` (fuente Mermaid) y `.pdf` renderizado.

---

## Metodología: Spec-Driven Development (SDD)

Este proyecto implementa el flujo **SDD** (Spec-Driven Development) documentado en los apuntes del curso:

### Flujo de Fases
```
Constitución → Spec (spec.md) → Clarificación → Plan (plan.md) → Tareas (tasks.md) → Implementación (TDD) → Validación → Cambio
```

### Reglas para Agentes
- **Consulta la constitución**: Lee siempre [`docs/constitution.md`](file:///home/gaston/Documentos/vscode/molu/docs/constitution.md) antes de proponer diseño o escribir código.
- **La Spec manda**: Ningún archivo en `cmd/` o `pkg/` se modifica sin una spec activa en `specs/NNN-<nombre>/`.
- **Desarrollo TDD por tareas**:
  - Implementa **una sola tarea** a la vez (`T1`, `T2`, etc.).
  - Escribe primero los tests (`*_test.go`).
  - Comprueba que fallan (rojo con `go test -v -race ./...`).
  - Escribe el código mínimo en Go para que pasen (verde con `go test -v -race ./...`).
  - Marca la tarea con `[x]` en `tasks.md` indicando los RF cubiertos.
  - **PÁRATE**. No avances a la siguiente tarea sin confirmación expresa del usuario.
- **Memoria del proyecto**: Actualiza [`MEMORY.md`](file:///home/gaston/Documentos/vscode/molu/MEMORY.md) al finalizar cada hito o spec.

---

## Convenciones de Código
- **Lenguaje**: Go 1.27+ estándar, SDK de MCP oficial, interfaces desacopladas para testing con mocks.
- **Idioma**: 
  - Código, comentarios en código, identificadores, firmas MCP y tests en **inglés**.
  - Documentación de specs, planes, tareas e interacción con el usuario en **español**.
