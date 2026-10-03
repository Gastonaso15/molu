# MEMORY.md — Memoria y Estado del Proyecto Molu & Hub

## Estado General
- **Proyecto**: Molu (Frontal MCP) + Hub de Seam (Registry MCP)
- **Documento rector**: *SEAM-CURE — Equipo IA / MCP: Detalle de Entregables* (12 de septiembre de 2026).
- **Metodología**: Spec-Driven Development (SDD) Spec-anchored.

---

## Lista Consolidada de Entregables (§2.3.2)

| Ítem | Alcance | Referencia Normativa | Estado |
| :--- | :--- | :--- | :--- |
| **molu — frontal MCP** | Completo | Especificación molu Parte 2, Paquete de trabajo §3.2 | En desarrollo (config, probe completado) |
| **Hub / Registry MCP de Seam** | Completo | Especificación molu Parte 3, Paquete de trabajo §3.1 | Por iniciar |
| **Taxonomía tipada de resultados** | Completo | Paquete de trabajo §3.3, especificación molu Parte 2 §8.5 | **Completado (Spec 001)** |
| **Telemetría y auditoría** | Completo | Paquete de trabajo §3.4 | Pendiente |
| **RPI — interfaz y proveedores V1** | Completo | Paquete de trabajo §3.6 (admit-all, static-context-rules, test double de Score) | Especificado en constitución |
| **Documentación** | Completo | Despliegue, configuración, runbook operativo, diagramas .mmd y .pdf | Pendiente |
| **Motor de Seam** | Parcial | Integración del publicador para funciones piloto Work Orders | Pendiente |

---

## Criterios de Aceptación Críticos a Demostrar
- **Criterio 1**: Cliente MCP de terceros sin modificar completa escenario punta a punta con rechazo de FSM visible en el camino.
- **Criterio 2**: Prueba de integración dedicada para cada una de las 6 filas de la taxonomía tipada (`NOT_OFFERED`, `NOT_FOUND`, `EMPTY`, `INVALID_TRANSITION`, `CONTRACT_VIOLATION`, `SUBSTRATE_UNAVAILABLE`).
- **Criterio 7**: Cambiar de proveedor RPI por configuración (`admit-all` vs `static-context-rules`) altera los resultados de descubrimiento sin tocar código.

---

## Specs Activas y Futuras

| Spec | Nombre / Descripción | Estado | Siguiente Paso |
| :--- | :--- | :--- | :--- |
| **001** | `taxonomy-types` — Tipos de taxonomía tipada (6 códigos, prefijos, verbatim) | **Completada** | Iniciar spec 002 (`xolu-probe`) |
| **002** | `xolu-probe` — Sonda de salud xolu (backoff, gating, SUBSTRATE_UNAVAILABLE) | **Completada** | Iniciar spec 003 (`schema-reader`) |

---

## Registro de Decisiones de Arquitectura (ADR)
- **ADR-001**: Uso de `go test -v -race ./...` como compuerta de validación en cada micro-tarea TDD.
- **ADR-002**: Desacoplamiento total entre lógica pura y llamadas I/O (xolu REST client y MCP transport) mediante interfaces.
- **ADR-003**: Inmutabilidad de los 6 códigos tipados de resultados: nunca colapsar en fallos genéricos.
- **ADR-004**: Neutralidad estricta: código agnóstico sin menciones a "Seam" en constantes, variables ni paquetes de molu o del Hub.
- **ADR-005**: Diagramas de secuencia versionados como texto `.mmd` (Mermaid) con exportación a `.pdf`.
- **ADR-006** (Spec 001): Paquete compartido `pkg/taxonomy/` interno con constructores puros, interfaces `XoluClient`, `MCPTransport`, `RPIProvider` mockables, `RetryMetadata` con attemptNumber/maxAttempts, formato `XOLU-<CODE>` fijo para xolu.

---
## Archivos Creados (Spec 001)

| Archivo | Descripción |
| :--- | :--- |
| `pkg/taxonomy/types.go` | Tipos base: `ErrorCode`, `TypedError`, `RetryMetadata`, prefijos, método `Error()` |
| `pkg/taxonomy/errors.go` | 6 constructores (`NewNotOffered`, `NewNotFound`, `NewEmpty`, `NewContractViolation`, `NewSubstrateUnavailable`, `NewSubstrateUnavailable`) + 3 wrappers verbatim (`WrapXoluNotFound`, `WrapXoluInvalidTransition`, `WrapXoluError`) + helpers `NowRFC3339`, `NextRetryRFC3339` |
| `pkg/taxonomy/errors_test.go` | 20+ tests unitarios: constructores, serialización JSON, wrappers, prefijos |
| `pkg/taxonomy/integration_test.go` | 9 tests de integración dedicados por cada código taxonomía + verbatim + no-colapso |
| `pkg/xolu/client.go` | Interfaz `XoluClient` (Walk, Find, Get, Ping) |
| `pkg/xolu/client_mock.go` | `MockXoluClient` con registro de llamadas |
| `pkg/mcp/transport.go` | Interfaz `MCPTransport` (Send, Receive, Close) |
| `pkg/mcp/transport_mock.go` | `MockMCPTransport` con registro de llamadas |
| `pkg/rpi/admit_score.go` | Interfaz `RPIProvider` (Admit, Score), structs `ToolCandidate`, `ScoredTool` |
| `pkg/rpi/providers/admit_all.go` | Proveedor V1 `AdmitAllProvider` (admite todo, score 1.0) |
| `pkg/rpi/providers/static_context_rules.go` | Proveedor V1 `StaticContextRulesProvider` (stub con reglas por tenant/rol) |
| `docs/diagrams/001-taxonomy-sequence.mmd` | Diagrama de secuencia Mermaid con todos los flujos de error tipado |

---

## Archivos Creados/Modificados (Spec 002)

| Archivo | Descripción |
| :--- | :--- |
| `pkg/exec/probe.go` | Integración con taxonomía: `Check()` retorna `*taxonomy.TypedError` con `SUBSTRATE_UNAVAILABLE` y `RetryMetadata` completo; `ProbeState` añade `AttemptNumber`/`MaxAttempts` |
| `pkg/exec/probe_test.go` | 10 tests unitarios: `Check()` taxonomía, healthy/nil, recovery reset backoff, concurrencia (-race), integración probe down → SUBSTRATE_UNAVAILABLE |
| `pkg/xolu/client_mock.go` | Añadido `ReadyFunc`/`Ready()` para implementar interfaz `Pinger` |
| `docs/diagrams/002-xolu-probe-sequence.mmd` | Diagrama de secuencia Mermaid: arranque, operación normal, fallo→backoff, recuperación, gating tool call |
