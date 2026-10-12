# MEMORY.md — Memoria y Estado del Proyecto Molu & Hub

## Estado General
- **Proyecto**: Molu (Frontal MCP) + Hub de Seam (Registry MCP)
- **Documento rector**: *SEAM-CURE — Equipo IA / MCP: Detalle de Entregables* (12 de septiembre de 2026).
- **Metodología**: Spec-Driven Development (SDD) Spec-anchored.

---

## Lista Consolidada de Entregables (§2.3.2)

| Ítem | Alcance | Referencia Normativa | Estado |
| :--- | :--- | :--- | :--- |
| **molu — frontal MCP** | Completo | Especificación molu Parte 2, Paquete de trabajo §3.2 | En desarrollo (config, probe, schema-reader completados) |
| **Hub / Registry MCP de Seam** | Completo | Especificación molu Parte 3, Paquete de trabajo §3.1 | Por iniciar |
| **Taxonomía tipada de resultados** | Completo | Paquete de trabajo §3.3, especificación molu Parte 2 §8.5 | **Completado (Spec 001)** |
| **Telemetría y auditoría** | Completo | Paquete de trabajo §3.4 | Pendiente |
| **RPI — interfaz y proveedores V1** | Completo | Paquete de trabajo §3.6 (admit-all, static-context-rules, test double de Score) | **Completado (Spec 004)** |
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
| **004** | `rpi-admit-score` — RPI Admit & Score (Eje 1: interfaz Admitter/Scorer, proveedores admit-all/static-context-rules, test double Score, Discover orquesta Admit→Score, config MOLU_FRONT_RPI_PROVIDER, Criterio 7, RF-14 NotOffered→taxonomy) | **Implementada (12/12 tasks)** | Iniciar spec 005 (Hub/Registry MCP) |

---

## Registro de Decisiones de Arquitectura (ADR)
- **ADR-001**: Uso de `go test -v -race ./...` como compuerta de validación en cada micro-tarea TDD.
- **ADR-002**: Desacoplamiento total entre lógica pura y llamadas I/O (xolu REST client y MCP transport) mediante interfaces.
- **ADR-003**: Inmutabilidad de los 6 códigos tipados de resultados: nunca colapsar en fallos genéricos.
- **ADR-004**: Neutralidad estricta: código agnóstico sin menciones a "Seam" en constantes, variables ni paquetes de molu o del Hub.
- **ADR-005**: Diagramas de secuencia versionados como texto `.mmd` (Mermaid) con exportación a `.pdf`.
- **ADR-006** (Spec 001): Paquete compartido `pkg/taxonomy/` interno con constructores puros, interfaces `XoluClient`, `MCPTransport`, `RPIProvider` mockables, `RetryMetadata` con attemptNumber/maxAttempts, formato `XOLU-<CODE>` fijo para xolu.
- **ADR-007** (Spec 004): Interfaces `Admitter` (obligatoria) y `Scorer` (opcional) separadas en `pkg/rpi/admit_score.go`; `RPIProvider` declara etapas vía `StagesImplemented()` bitmask; `ProviderRegistry` en `pkg/rpi/providers/registry.go` evita ciclo de imports y valida coherencia `StageScore`↔`Scorer`.
- **ADR-008** (Spec 004): `Discover` en `pkg/rpi/discover.go` orquesta Eje 1: `Admit` → si `StagesImplemented()&StageScore` → `Score` (solo admitidos) → `DiscoveryResult{Offered, NotOffered}`; error proveedor → `taxonomy.NewSubstrateUnavailable` con `Details["component"]="rpi-provider"`, sin inventar timestamps.
- **ADR-009** (Spec 004): `Candidate` = referencia `namespace.Nombre` (no copias); `Offered` = solo admitidos reordenados; `NotOffered` = rechazados con `Reason`; `Score` reordena sin exponer `score`/`rationale` (cumple §2.5).
- **ADR-010** (Spec 004): `static-context-rules` carga JSON al arranque, denegar por defecto si no hay regla para `(tenant, rol)`, sin comodines en V1; `admit-all` admite todos; `score-test-double` solo en tests.
- **ADR-011** (Spec 004): Config RPI estilo actual (`LoadFromEnv`, `validate() []error`, `reader.str`, `errors.Join`); `MOLU_FRONT_RPI_PROVIDER` default `static-context-rules` requiere `MOLU_FRONT_RPI_RULES_FILE`; arranque en `main.go` valida proveedor y hace `os.Exit(1)` si falla.
- **ADR-012** (Spec 004): Helper `NotOfferedToTaxonomyError(tenant, candidateID)` en `pkg/rpi/admit_score.go` convierte `NotOffered` a `taxonomy.NewNotOffered` con código `XOLU-MOLU-FRONT-NOT_OFFERED`; test de integración verifica código exacto y que no es `NOT_FOUND`.

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
 
---
 
## Archivos Creados/Modificados (Spec 003)
 
| Archivo | Descripción |
| :--- | :--- |
| `pkg/schema/types.go` | Tipos de dominio: `PrimitiveSchema`, `StateMachine`, `Transition`, `PayloadType`, `Schemas` con tags JSON |
| `pkg/schema/client.go` | Interfaz `SchemaClient` + `XoluSchemaClient` (HTTP con timeouts, manejo 5xx/JSON inválido) |
| `pkg/schema/loader.go` | `SchemaLoader` completo: `Load` (retry/backoff), `Refresh`, `GetPrimitives`, `Run` (ticker), `Stop`, `swap` atómico con RWMutex |
| `pkg/schema/validation.go` | `ValidateInput` con `gojsonschema` → `ValidationResult` (valid/invalidFields), sin ciclo de imports |
| `pkg/schema/loader_test.go` | 15+ tests unitarios: carga, retry/backoff, max attempts, context cancellation, refresh atómico, diff add/remove, validación (éxito, required, type mismatch, enum, nested), concurrencia (-race) |
| `pkg/schema/client_test.go` | 5 tests: timeout, 5xx, JSON inválido, estructura inesperada, concurrencia loader+refresh |
| `pkg/config/config.go` | Añadidas `MOLU_FRONT_SCHEMA_REFRESH_INTERVAL`, `MOLU_FRONT_SCHEMA_RETRY_FLOOR`, `MOLU_FRONT_SCHEMA_RETRY_CEILING`, `MOLU_FRONT_SCHEMA_MAX_ATTEMPTS`, `MOLU_FRONT_SCHEMA_TIMEOUT` con defaults y validación |
| `pkg/config/config_test.go` | Tests para nuevos defaults y validación (floor > ceiling, maxAttempts negativo) |
| `cmd/molu/main.go` | Wiring: `Load()` al arranque, `Run()` en goroutine, SIGHUP handler → `Refresh()`, registro primitivos sin namespace |
| `pkg/taxonomy/integration_test.go` | Nuevo `TestIntegration_ContractViolation_InvalidInput`: flujo E2E schema validation → `CONTRACT_VIOLATION` con `details.fields[]` (Criterio 2) |
| `pkg/taxonomy/errors_test.go` | Helpers `requireTrue`, `requireFalse` |
| `docs/diagrams/003-schema-reader-sequence.mmd` | Diagrama Mermaid: arranque+retry, carga exitosa, registro MCP, tool call válido, tool call inválido → CONTRACT_VIOLATION, hot-reload SIGHUP, hot-reload intervalo |

---

## Archivos Creados/Modificados (Spec 004)

| Archivo | Descripción |
| :--- | :--- |
| `pkg/rpi/admit_score.go` | **Reemplazado**: interfaces `Admitter`, `Scorer`, tipos `Stage`, `AdmitQuery`, `ScoreQuery`, `Candidate`, `AdmittedCandidate`, `AdmissionResult`, `DiscoveryResult`, `RPIProvider`; eliminados `ToolCandidate`, `ScoredTool`, vieja `RPIProvider` |
| `pkg/rpi/admit_score_test.go` | **Creado**: tests de tipos y bitmask `Stage` |
| `pkg/rpi/providers/admit_all.go` | **Reemplazado**: `AdmitAllProvider` implementa `Admitter`; factory `NewAdmitAllProvider` devuelve `(rpi.RPIProvider, error)` |
| `pkg/rpi/providers/admit_all_test.go` | **Creado**: tests unitarios admisión total, orden estable, factory returns interface |
| `pkg/rpi/providers/static_context_rules.go` | **Reemplazado**: `StaticContextRulesProvider` implementa `Admitter`; carga JSON, filtra `(tenant, rol)`, denegar por defecto, orden estable |
| `pkg/rpi/providers/static_context_rules_test.go` | **Creado**: 8 tests: carga JSON, filtro tenant/rol, denegar por defecto, catálogo vacío, orden estable, factory returns interface |
| `pkg/rpi/providers/score_test_double.go` | **Creado**: `ScoreTestDouble` implementa `Scorer` + opcional `Admitter` delegando; `SetError`, `SetAdmitDelegate`, `StagesImplemented()` |
| `pkg/rpi/providers/score_test_double_test.go` | **Creado**: 6 tests: reordena, vacío, no toca otro tenant, error controlado, delega Admit, stages |
| `pkg/rpi/providers/registry.go` | **Creado**: `ProviderRegistry` en `pkg/rpi/providers` (evita ciclo); `Create` valida coherencia `StageScore`↔`Scorer`; `KnownProviders()` |
| `pkg/rpi/providers/registry_test.go` | **Creado**: 6 tests: create admit-all/static, unknown, sin rules file, valida coherencia, known providers |
| `pkg/rpi/providers/integration_test.go` | **Creado**: 5 tests integración: conformidad §2.5 (ambos proveedores), Criterio 7, NOT_OFFERED, EMPTY, SUBSTRATE_UNAVAILABLE con `component=rpi-provider` |
| `pkg/rpi/discover.go` | **Creado**: `Discover(ctx, provider, query, candidates) (DiscoveryResult, error)` orquesta Admit→Score; `ErrProviderUnavailable`; `toSubstrateUnavailable` con `component=rpi-provider` |
| `pkg/rpi/discover_test.go` | **Creado**: 8 tests: AdmitOnly, WithNotOffered, WithScorer, ProviderUnavailable (Admit/Score), ScoreNotDeclared_NoCall, EmptyCatalog, ScoreOnlyAdmitted |
| `pkg/config/config.go` | **Modificado**: añadidos `MOLU_FRONT_RPI_PROVIDER` (default `static-context-rules`), `MOLU_FRONT_RPI_RULES_FILE`; validación en `validate()` estilo actual |
| `pkg/config/config_test.go` | **Modificado**: 6 tests nuevos: defaults, admit-all, provider inválido, static requiere rules file, archivo inexistente (config no falla, falla en main) |
| `pkg/config/testdata/rules.json` | **Creado**: JSON de ejemplo para tests (tenant1/admin/user, tenant2/admin) |
| `pkg/rpi/providers/testdata/rules.json` | **Creado**: copia para tests de integración (ruta relativa) |
| `cmd/molu/main.go` | **Modificado**: crea `ProviderRegistry` y proveedor RPI al arrancar; error → log + `os.Exit(1)`; guarda proveedor para wiring futuro Hub |
| `docs/diagrams/004-rpi-admit-score-sequence.mmd` | **Creado**: Diagrama Mermaid: arranque validación proveedor → descubrimiento → Admit → Score (si StageScore) → DiscoveryResult → Offered/NotOffered/EMPTY/SUBSTRATE_UNAVAILABLE |
| `docs/diagrams/004-rpi-admit-score-sequence.pdf` | **Pendiente**: renderizado con `mmdc` (no disponible en entorno; generable en CI/local) |

---

## Criterios de Aceptación Demostrados (Spec 004)

- **Criterio 2** (parcial): Tests de integración dedicados para `NOT_OFFERED`, `EMPTY`, `SUBSTRATE_UNAVAILABLE` (Eje 1). `NOT_FOUND`, `CONTRACT_VIOLATION`, `INVALID_TRANSITION` son Eje 2 (fuera de alcance).
- **Criterio 7**: `TestIntegration_Criterion7_ProviderSwitch` verifica que mismo `AdmitQuery` + mismos candidatos con `admit-all` vs `static-context-rules` (seleccionados por `MOLU_FRONT_RPI_PROVIDER`) producen `DiscoveryResult` distintos.
