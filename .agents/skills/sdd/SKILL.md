---
name: sdd
description: Úsala siempre que trabajes con Spec-Driven Development en este proyecto (docs/constitution.md o cualquier archivo dentro de specs/) - redactar, revisar o cambiar specs, planes y tareas, o implementar y validar tareas de una spec.
---

# Spec-Driven Development (SDD) para Molu & Hub

Esta guía define el flujo de trabajo guiado por especificaciones (Spec-Driven Development) adaptado al backend y servidor MCP **Molu** y al **Hub de Seam** en Go, en estricto cumplimiento del documento normativo *Detalle de Entregables*.

---

## Principios del Flujo
1. **Constitución → Spec → Clarificación → Plan → Tareas → Implementación → Validación → Cambio.**
2. **Nunca pases a la siguiente fase sin la aprobación explícita del usuario.**
3. **La spec manda**: si algo no está en la spec activa, no se implementa. El piso normativo del proyecto son los entregables y criterios de aceptación del documento de entregables.
4. **Un cambio de requisitos** se formaliza primero en la spec, luego en el plan y las tareas, y por último en el código.
5. **Cada spec vive en su propia carpeta**: `specs/NNN-nombre/` conteniendo `spec.md`, `plan.md` y `tasks.md`.
   - **Persistencia obligatoria en disco**: Cada vez que el agente redacte o genere una spec (`spec.md`), plan (`plan.md`) o tareas (`tasks.md`), **DEBE guardarlo directamente en disco en su respectiva ruta** (`specs/NNN-nombre/spec.md`), creando la carpeta si no existe. Está estrictamente prohibido emitir el contenido en el chat pidiendo al usuario que lo copie y pegue manualmente.
6. **Al terminar cada fase**, actualiza `MEMORY.md`.

---

## Plantilla de Especificación (`specs/NNN-nombre/spec.md`)

```markdown
# Spec NNN — <Nombre de la funcionalidad>

Estado: borrador | aprobada | implementada

## Contexto y objetivo
<Qué problema resuelve y por qué merece la pena. Un párrafo.>

## Usuarios / Actores
<Quién o qué interactúa con esta funcionalidad: Agente de IA (Claude, Copilot), cliente MCP externo, publicador del Hub, etc.>

## Historias de usuario
- HU-1: Como <rol>, quiero <acción> para <beneficio>.

## Ejes de Control Involucrados
- **Eje 1 (Qué se ofrece)**: <Cómo interviene el Hub y el proveedor RPI (Admit/Score) para tenant y rol.>
- **Eje 2 (Qué puede ejecutarse)**: <Cómo intervienen las FSMs y guardas de xolu mediante el primitivo walk o llamadas directas.>

## Convención de Nombres de Herramienta
- <Indicar si son primitivos genéricos (NUNCA llevan namespace, e.g. 'walk', 'find') o funciones de dominio (SIEMPRE llevan 'namespace.Nombre', e.g. 'work_orders.CreateOrder').>

## Definiciones
<Glosario de términos clave, formatos de payload o contratos de funciones.>

## Requisitos funcionales (en notación EARS)
- RF-1: CUANDO <evento>, EL SISTEMA <respuesta>.
- RF-2: SI <objeto no existe para este tenant>, ENTONCES EL SISTEMA <retorna NOT_FOUND>.
- RF-3: SI <FSM rechaza la transición>, ENTONCES EL SISTEMA <retorna INVALID_TRANSITION preservando el diagnóstico verbatim de xolu>.
- RF-4: SI <entrada incumple el schema JSON>, ENTONCES EL SISTEMA <retorna CONTRACT_VIOLATION listando los campos infractores>.
- RF-5: SI <xolu es inalcanzable>, ENTONCES EL SISTEMA <retorna SUBSTRATE_UNAVAILABLE con último contacto exitoso y próximo reintento>.
- RF-6: SI <función/entidad está fuera de alcance para el tenant/rol>, ENTONCES EL SISTEMA <retorna NOT_OFFERED>.
- RF-7: MIENTRAS <estado>, EL SISTEMA <respuesta>.
- RF-8: EL SISTEMA <comportamiento permanente>.

> Nota sobre la taxonomía: Nunca colapsar los resultados en errores genéricos. Usar estrictamente los 6 códigos tipados (§2.4): NOT_OFFERED, NOT_FOUND, EMPTY, INVALID_TRANSITION, CONTRACT_VIOLATION, SUBSTRATE_UNAVAILABLE.

## Requisitos no funcionales
- Concurrencia segura y pase obligatorio de `go test -v -race ./...`.
- Neutralidad de dominio: prohibido usar la palabra "Seam" en el código, constantes o defaults.
- Rendimiento, memoria y compatibilidad Go 1.27+.

## Casos límite
<Desconexión de xolu con backoff, reintentos hasta MOLU_FRONT_STARTUP_MAX_ATTEMPTS, schemas vacíos o cambiantes, carreras de datos.>

## Fuera de alcance
<Lo que explícitamente NO se aborda en esta iteración.>

## Criterios de finalización
- Todos los RF cubiertos con tests unitarios en verde (`go test -v -race ./...`).
- Pruebas de integración dedicadas para cada fila de la taxonomía tipada que aplique.
- Diagrama de secuencia Mermaid (`.mmd` y `.pdf`) generado para los flujos principales.

## Dudas abiertas
- [NECESITA ACLARACIÓN] <duda o decisión pendiente del usuario>
```

---

## Plantilla de Plan Técnico (`specs/NNN-nombre/plan.md`)

```markdown
# Plan Técnico: Spec NNN — <Nombre>

## Archivos y Responsabilidades
- `pkg/<paquete>/<archivo>.go`: Qué responsabilidad asume y qué interfaces implementa.
- `pkg/<paquete>/<archivo>_test.go`: Casos de prueba unitarios, de concurrencia y mocks.
- `docs/diagrams/<nombre>.mmd`: Diagrama de secuencia fuente en Mermaid.

## Modelos y Contratos
- Structs de datos en Go.
- Definición de herramientas MCP (schema JSON, parámetros requeridos y tipos).
- Integración con la taxonomía tipada: códigos de error con prefijo `XOLU-MOLU-FRONT-*` vs `XOLU-*` verbatim.

## Funciones Puras e Interfaces Desacopladas
- Interfaces para el cliente de xolu y transportes MCP para posibilitar testing con mocks.
- Contrato RPI: implementación de `Admit(context, query, candidates)` y `Score(context, query, admitted)`.

## Diagrama de Secuencia (Mermaid)
```mermaid
sequenceDiagram
    participant Agent as Agente (cliente MCP)
    participant Molu as molu (servidor MCP)
    participant Hub as Hub (catálogo)
    participant Xolu as xolu (sustrato)
    ...
```

## Concurrencia y Resiliencia
- Manejo de `context.Context` (timeouts y cancelación).
- Ausencia total de data races (`-race`).
- Reintentos con retroceso exponencial (`MOLU_FRONT_PING_FAIL_FLOOR` a `MOLU_FRONT_PING_FAIL_CEILING`).

## Decisiones Técnicas y Alternativas Descartadas
- **Decisión 1**: <Justificación técnica>.
  - *Alternativa descartada*: <Por qué se rechazó>.

## Matriz de Cobertura de Requisitos
- RF-1 cubierto por: `pkg/...` y test `Test...`
- RF-2 (Taxonomía NOT_FOUND) cubierto por: test de integración `TestTaxonomy_NotFound`
- ...

## Estrategia de Tests
- Unitarios: `go test -v -race ./pkg/...`
- Integración: validación de los códigos de la taxonomía (§2.4) y verificación de contrato RPI (§2.5).
```

---

## Plantilla de Tareas (`specs/NNN-nombre/tasks.md`)

```markdown
# Tareas: Spec NNN — <Nombre>

- [ ] **T1. <Descripción corta de la tarea>.** RF-1, RF-2
  - Hecho cuando: <Comprobación verificable con `go test -v -race`>.

- [ ] **T2. <Prueba de integración para taxonomía tipada>.** RF-3, RF-4
  - Hecho cuando: <Test dedicado de integración para los códigos tipados pasa en verde>.

- [ ] **T3. <Diagrama de secuencia y documentación>.** RF-x
  - Hecho cuando: <Archivo .mmd creado y compilado a .pdf mediante mmdc>.
```

- Cada tarea debe insumir entre **20 y 30 minutos**.
- Deben ordenarse en riguroso orden de dependencia.
- Si una spec produce más de 8-10 tareas, propón dividir la funcionalidad en dos specs.

---

## Implementación TDD (Paso a Paso)

Para cada tarea individual:
1. **Tests primero (Rojo)**: Escribe el test unitario o de integración en `pkg/.../*_test.go` y ejecuta `go test -v -race ./...` para verificar que falla.
2. **Código mínimo (Verde)**: Implementa la lógica en Go hasta que el test pase limpiamente.
3. **Verificación de concurrencia**: Ejecuta `go test -v -race ./...` asegurando cero data races.
4. **Marcar y Parar**: Marca la tarea con `[x]` en `tasks.md`, actualiza los RF cubiertos y **DETÉN LA EJECUCIÓN**. No inicies la siguiente tarea sin la aprobación del usuario.
