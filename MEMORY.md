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
| **Hub / Registry MCP de Seam** | Completo | Especificación molu Parte 3, Paquete de trabajo §3.1 | En desarrollo (Spec 005: registry thread-safe y API de publicadores completados) |
| **Taxonomía tipada de resultados** | Completo | Paquete de trabajo §3.3, especificación molu Parte 2 §8.5 | **Completado (Spec 001)** |
| **Telemetría y auditoría** | Completo | Paquete de trabajo §3.4 | **Completado (Spec 007: 30/30 tasks, coverage 72.8%)** |
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
| **003** | `schema-reader` — Lectora de schemas xolu (GET /schemas, walk/find/get sin namespace, hot-reload, CONTRACT_VIOLATION) | **Completada (18/18 tasks)** | Iniciar spec 004 (`rpi-admit-score`) |
| **004** | `rpi-admit-score` — Interfaz RPI (`Admit`/`Score`) y proveedores V1 (`admit-all`, `static-context-rules`) | **En curso**: plan.md y tasks.md cubren T-004.2 a T-004.5; T1–T9 hechas (interfaz `RPIProvider` + mock, `admit-all`, `static-context-rules`, `rpi.Offer` + integración `NOT_OFFERED`). Implementación previa revertida (`457bbd1`, `3aa5903`) | Pendientes de sus responsables: config `MOLU_FRONT_RPI_*` (RF-1), `tools/list` (RF-2, RF-3, RF-8), Criterio 7 y diagrama; cada uno agrega sus tareas a `tasks.md` |
| **005** | `catalog-and-hub` — Hub/Registry MCP: catálogo, API de publicadores, `tools/list` filtrado por RPI, `tools/call` stub | **En curso** (spec en borrador; `Registry` y `PublisherAPI` implementados) | Añadir `tools/list` con Admit+Score (depende de spec 004), `tools/call` stub, config `MOLU_HUB_*`, wiring MCP y diagrama |
| **006** | `mcp-server-stdio` — Servidor MCP Stdio y Despachador de Primitivos en Molu Frontend (ampliada a los 13 primitivos genéricos §5.1) | **En curso (7/8 tasks)** | Continuar con T8 (diagrama de secuencia y documentación operativa) |
| **007** | `telemetry-and-observability` — Logging 4 dominios, correlación request_id/trace_id, rotación, security log, health/metrics Prometheus, middleware MCP | **Completada (30/30 tasks)** | Iniciar spec 008 (si aplica) / documentación pendiente T23-T28 |



---

## Registro de Decisiones de Arquitectura (ADR)
- **ADR-001**: Uso de `go test -v -race ./...` como compuerta de validación en cada micro-tarea TDD.
- **ADR-002**: Desacoplamiento total entre lógica pura y llamadas I/O (xolu REST client y MCP transport) mediante interfaces.
- **ADR-003**: Inmutabilidad de los 6 códigos tipados de resultados: nunca colapsar en fallos genéricos.
- **ADR-004**: Neutralidad estricta: código agnóstico sin menciones a "Seam" en constantes, variables ni paquetes de molu o del Hub.
- **ADR-005**: Diagramas de secuencia versionados como texto `.mmd` (Mermaid) con exportación a `.pdf`.
- **ADR-006** (Spec 001): Paquete compartido `pkg/taxonomy/` interno con constructores puros, interfaces `XoluClient`, `MCPTransport`, `RPIProvider` mockables, `RetryMetadata` con attemptNumber/maxAttempts, formato `XOLU-<CODE>` fijo para xolu.
- **ADR-007** (Spec 007): Librería `zerolog` para logging estructurado (API fluida, zero-allocation JSON, mejor performance que zap); UUID v7 (ULID) para `request_id` (timestamp ordenable); W3C `traceparent` para `trace_id`/`span_id` (estándar industria); Security log separado con flush síncrono (integridad auditoría); Puerto telemetry 9090 desacoplado del MCP; `LogRotator` propia para control total (tamaño+tiempo, compresión, security log independiente); Prometheus client nativo para métricas; Health check con cache 30s basado en último ping exitoso xolu.

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
| `pkg/rpi/admit_score.go` | Interfaz `RPIProvider` (Admit, Score), structs `ToolCandidate`, `ScoredTool` (reescrito en spec 004) |
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
| `pkg/schema/client.go` | Interfaz `SchemaClient` + `XoluSchemaClient` (HTTP con timeouts, consulta a `/api/v1/schemas`, manejo 5xx/JSON inválido) |
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

## Archivos Creados (Spec 005, sesión 14-oct-2026)

| Archivo | Descripción |
| :--- | :--- |
| `pkg/hub/registry.go` | Commit `92355cf`. `ToolRegistration` y `Registry` en memoria, thread-safe (`sync.RWMutex`, índice por publicador + índice plano por nombre). `Register` valida convención de nombres (primitivo sin namespace, dominio `namespace.Nombre`), es idempotente por `(publisher_id, name)` y rechaza con `CONTRACT_VIOLATION` un nombre ya tomado por otro publicador. `Unregister` → `NOT_FOUND` con `details {publisher_id, name}`. `Lookup`/`List` devuelven copias profundas, ordenadas por nombre, `List` nunca retorna `nil`. Cubre RF-2, RF-3, RF-8 y RF-10 |
| `pkg/hub/registry_test.go` | Tests: registro (dominio y primitivo), payloads inválidos, duplicado idempotente, nombre de otro publicador, unregister/not found, listado por publicador y global, catálogo vacío, aislamiento de copias, acceso concurrente (-race) |
| `pkg/hub/publisher_api.go` | Commit `98a5411`. `PublisherAPI` agnóstica de transporte con las herramientas MCP `registry.register`, `registry.unregister`, `registry.list` (`Tools()` entrega descriptores con JSON Schema en orden estable; `Handle(method, params)` despacha). `decodeParams` es estricto (JSON mal formado, no-objeto, datos sobrantes, campos desconocidos o de tipo incorrecto → `CONTRACT_VIOLATION` con el campo infractor). Método desconocido → `NOT_OFFERED`. `registry.list` es vista administrativa sin filtro RPI y devuelve `{"tools":[]}` si está vacío |
| `pkg/hub/publisher_api_test.go` | Tests: register idempotente, violaciones de contrato por tabla, unregister y su `NOT_FOUND`, list administrativo (params vacío/null/`{}`), catálogo vacío, campo desconocido, método no ofrecido, descriptores de `Tools()` |


## Archivos Creados/Modificados (Spec 004, T-004.2 y T-004.3, sesión 16-oct-2026)

| Archivo | Descripción |
| :--- | :--- |
| `specs/004-rpi-admit-score/plan.md` | Plan técnico limitado a T-004.2 y T-004.3. Decisiones: `Admit`/`Score` devuelven `error` (D1), parámetro `actx` (D2), mock en `pkg/rpi` (D3), admit-all devuelve slice nuevo (D4), catálogo vacío → slice vacío no `nil` (D5) |
| `specs/004-rpi-admit-score/tasks.md` | T1–T4 completadas, cada una etiquetada con su tarjeta de Trello |
| `pkg/rpi/admit_score.go` | Tipos de la spec: `ToolCandidate` (+`Primitive`), `AdmitContext {Tenant, Roles, Query}`, `ScoredTool` (+`Rationale`); `RPIProvider` con `Admit/Score(ctx, AdmitContext, ...)`. Definiciones, RF-10 |
| `pkg/rpi/admit_score_test.go` | Nombres JSON de los tipos y embebido de `ToolCandidate` en `ScoredTool` |
| `pkg/rpi/provider_mock.go` | `MockRPIProvider` thread-safe (`AdmitFunc`/`ScoreFunc`, `AdmitCalls`/`ScoreCalls`, `Reset`), estilo `MockXoluClient`. RF-9 |
| `pkg/rpi/provider_mock_test.go` | Delegación, errores, "not implemented", `Reset` y concurrencia (-race) |
| `pkg/rpi/providers/admit_all.go` | `AdmitAllProvider` con firmas nuevas y aserción `rpi.RPIProvider`: `Admit` devuelve copia de todo, `Score` 1.0 conservando orden. RF-5, RF-7, RF-9 |
| `pkg/rpi/providers/admit_all_test.go` | Admite todo, catálogo vacío/`nil`, aislamiento de la entrada, Score 1.0 y orden, concurrencia (-race) |

Merge con spec 007 (`658def3`): `RPIProvider` conserva `SetLogger`/`SetMetrics` de la spec 007; `AdmitWithLogging`/`ScoreWithLogging` adaptados a `AdmitContext`; admit-all y `MockRPIProvider` implementan `SetLogger`, `SetMetrics`, `Logger()`, `Metrics()` (plan D6).

`pkg/rpi/providers/static_context_rules.go` no se tocó: conserva las firmas viejas (compila porque no declara la interfaz) y se adapta en T-004.4. `go test ./...` pasa completo. Con `-race` fallan `pkg/schema` (`TestLoaderConcurrency`) y `pkg/telemetry`/`pkg/telemetry_integration`: carreras preexistentes en `main`, no de la spec 004.

## Archivos Creados/Modificados (Spec 004, T-004.4 y T-004.5, sesión 21-oct-2026)

| Archivo | Descripción |
| :--- | :--- |
| `specs/004-rpi-admit-score/plan.md` / `tasks.md` | Ampliados a T-004.4 y T-004.5 (T5–T9). Decisiones D7 (`static-context-rules` implementa `Score` 1.0/0.0 por RF-6), D8 (claves `"tenant:rol"`), D9 (`rpi.Offer` en `pkg/rpi`) |
| `pkg/rpi/providers/static_context_rules.go` | `StaticContextRulesProvider` adaptado a `RPIProvider`: reglas `"tenant:rol"` → nombres, coincidencia exacta y case-sensitive, denegar por defecto, `Score` 1.0/0.0 con orden estable, telemetría como admit-all. `ParseStaticContextRules` lee el JSON de reglas. RF-5, RF-6, RF-9 |
| `pkg/rpi/providers/static_context_rules_test.go` | Formato de reglas, admisión, tests de rechazo (otra herramienta, tenant, rol, sin roles, tenant vacío, sin reglas, mayúsculas), aislamiento, `Score`, concurrencia (-race) |
| `pkg/rpi/offer.go` / `offer_test.go` | `rpi.Offer`: herramienta excluida por `Admit` → `taxonomy.NewNotOffered(tenant, tool)`; errores del proveedor sin cambios. RF-4 |
| `pkg/rpi_integration/not_offered_test.go` | Integración (Criterio 2): herramienta existente en `hub.Registry` rechazada por `static-context-rules` → exactamente `XOLU-MOLU-FRONT-NOT_OFFERED` (también en JSON); con rol permitido o admit-all se ofrece |

`go test -race ./...` pasa completo (Linux). Leer las reglas desde `MOLU_FRONT_RPI_RULES_FILE`/`MOLU_FRONT_RPI_RULES_JSON` y elegir el proveedor (RF-1) queda para su responsable; `ParseStaticContextRules` ya está disponible para eso. Quién llama a `rpi.Offer` en `tools/call` lo decide la tarjeta de ensamblado del Hub.

## Archivos Creados/Modificados (CI y Herramientas, sesión 15-oct-2026)

| Archivo | Descripción |
| :--- | :--- |
| `.github/workflows/ci.yml` | Workflow de GitHub Actions (`CI - Build & Test`) para verificar compilación (`make build`) y dependencias (`go mod verify`) automáticamente en cada push y pull request a `main`/`master`. |
| `makefile` | Añadido target `inspector` para compilar e iniciar directamente `@modelcontextprotocol/inspector` inyectando las variables de entorno de molu mediante flags `-e` (`MOLU_FRONT_XOLU_URL`, `MOLU_FRONT_XOLU_AUTH_MODE`, `MOLU_FRONT_XOLU_TOKEN_FILE` con ruta absoluta a `.token.dev`). |
| `.gitignore` | Añadido `.token.dev` y `*.dev` para evitar versionar tokens de prueba locales. |
| `README.md` | Creado README principal en la raíz con resumen del proyecto y guía paso a paso para ejecutar con MCP Inspector (vía `make inspector`, `npx` directo con flags `-e`, `go run` y UI web con variables de entorno mínimas requeridas). |
| `docs/README.md` | Actualizado con sección de inicio rápido para MCP Inspector y referencia al README raíz. |


**Pendiente de Spec 005**: `tools/list` con RPI (RF-4, RF-5, RF-6, RF-9), `tools/call` stub `NOT_OFFERED` (RF-7), carga del proveedor RPI y variables `MOLU_HUB_*` (RF-1), wiring MCP en `cmd/`, pruebas de integración de Criterios 2 y 7, diagrama `docs/diagrams/005-*.mmd` y `.pdf`. No existe aún `plan.md` ni `tasks.md` de la spec 005. La interfaz RPI de la spec 004 (T-004.2) ya está disponible para `tools/list`.

---

## Archivos Creados (Spec 007)

| Archivo | Descripción |
| :--- | :--- |
| `specs/007-telemetry-and-observability/spec.md` | Especificación funcional EARS con 14 RF: logging 4 dominios (sistema, aplicación, seguridad, red/acceso), correlación request_id (ULID/UUID v7) + trace_id (W3C traceparent), rotación por tamaño/tiempo, security log append-only flush síncrono, health/metrics en puerto 9090, propagación headers a xolu, ADR-007 |
| `specs/007-telemetry-and-observability/plan.md` | Plan técnico: 20+ archivos Go (`pkg/telemetry/*`, integración en `pkg/xolu`, `pkg/rpi`, `cmd/molu`), modelos/contratos, mapeo RF→implementación, decisiones técnicas (ADR-007), matriz de tests unitarios/integración/concurrencia |
| `specs/007-telemetry-and-observability/tasks.md` | 30 micro-tareas TDD (T1–T30) con criterios de aceptación "rojo→verde", cubriendo todos los RF y entregables de documentación |
| `pkg/telemetry/types.go` | Tipos base: `LogDomain` (system/application/security/network), `LogLevel`, `LogEntry` con campos base obligatorios, `RequestContext` con correlation IDs, `RetryMetadata` (reutiliza de taxonomy) |
| `pkg/telemetry/logger.go` | Interfaz `Logger` + implementación `zerologLogger` con 4 dominios, `WithContext`/`WithFields`, niveles configurables, security log flush síncrono, rotación integrada |
| `pkg/telemetry/logger_test.go` | 10 tests unitarios: JSON/console format, niveles, dominios, security sync, WithContext, WithFields, Level getter/setter, zero-alloc benchmark |
| `pkg/telemetry/rotation.go` | `LogRotator` con rotación por tamaño (configurable MB) + tiempo (días), compresión gzip async, limpieza por antigüedad, concurrencia segura (mutex), no pérdida de datos |
| `pkg/telemetry/rotation_test.go` | 6 tests: rotación por tamaño/tiempo, retención, compresión, concurrencia (-race), no pérdida de datos |
| `pkg/telemetry/metrics.go` | Interfaz `Metrics` + implementación Prometheus: contadores peticiones (component/method/tool/code), histogramas latencia (component/method), contadores errores (component/code), handler `/metrics` |
| `pkg/telemetry/metrics_test.go` | 4 tests con registry custom: contadores, histogramas, handler, multi-componente |
| `pkg/telemetry/health.go` | Interfaz `Health` + implementación: `/healthz` (ready/live), estado xolu basado en último ping exitoso (cache 30s), devuelve 503 si stale >30s |
| `pkg/telemetry/health_test.go` | 5 tests: ready/live inicial, xolu available/unavailable, métodos Ready/Live, cache TTL |
| `pkg/telemetry/middleware.go` | Middleware MCP: genera `request_id` (ULID), extrae/propaga `trace_id`/`span_id` (W3C traceparent), parsea method/tool del body JSON, setea headers `X-Request-ID`, `X-Trace-ID` en respuesta |
| `pkg/telemetry/middleware_test.go` | 6 tests: request_id (ULID), traceparent extraction/generation, propagación context, method/tool extraction, response headers |
| `pkg/telemetry/config.go` | Estructuras `Config` + `DefaultConfig` + `LoadConfigFromEnv`: todas las variables `MOLU_FRONT_LOG_*`, `MOLU_FRONT_TELEMETRY_*`, `MOLU_FRONT_SECURITY_LOG_*` con defaults y validación |
| `pkg/telemetry/config_test.go` | 4 tests: defaults, env parsing, validación (format/level/component/ports), telemetry defaults |
| `pkg/xolu/client.go` | `HTTPXoluClient` con logging integrado: `Walk`/`Find`/`Get`/`Ping` loggean latencia y código taxonomía, `Ready` para probe apuntando a `GET /ready` (endpoint oficial de readiness de xolu en Spec 002), inyección de `Logger` y `Metrics` |
| `pkg/rpi/admit_score.go` | Helpers `AdmitWithLogging`/`ScoreWithLogging`: log aplicaciones/seguridad con latencia, decision DENY si Admit vacío, integración con `Logger` y `Metrics` opcionales |
| `cmd/molu/main.go` | Wiring completo: `Logger`, `Metrics`, `Health`, servidor telemetry HTTP en puerto 9090 (`/metrics`, `/healthz`), `xolu.HTTPXoluClient` con logger/metrics, probe con `Ready`, health check periódico xolu, graceful shutdown |
| `pkg/telemetry_integration/integration_test.go` | 6 tests de integración: flujo MCP completo (4 dominios + métricas), error taxonomía + request_id, security auth DENY, xolu unavailable + health 503, shutdown, rotación bajo carga (-race) |
| `docs/deployment/007-telemetry-deployment.md` | Despliegue: orden tolerado, puertos (MCP 8080 / telemetry 9090), reintentos backoff, transportes stdio/streamable-http, verificaciones post-despliegue |
| `docs/configuration/007-telemetry-config.md` | Configuración: lista exhaustiva `MOLU_FRONT_LOG_*`, `MOLU_FRONT_TELEMETRY_*`, `MOLU_FRONT_SECURITY_LOG_*`, `MOLU_FRONT_XOLU_*`, defaults y justificación operativa por ambiente |
| `docs/runbook/007-telemetry-runbook.md` | Runbook: diagnóstico por 4 dominios, correlación request_id/trace_id (ULID/W3C), códigos taxonomía en logs, procedimiento SUBSTRATE_UNAVAILABLE, métricas Prometheus, health check, rotación |
| `docs/diagrams/007-telemetry-sequence.mmd` | Diagrama secuencia: petición MCP → middleware → 4 dominios + métricas + health check |
| `docs/diagrams/007-telemetry-error-sequence.mmd` | Diagrama error taxonomía: NOT_FOUND → correlación request_id en details + log network/error + métrica error |
| `docs/diagrams/007-telemetry-health-metrics.mmd` | Diagrama health/metrics: scrape /metrics, health check cache 30s, ping periódico xolu, backoff arranque |

---243

## Archivos Creados / Modificados (Spec 006 & Endpoints Xolu, sesión 16-oct-2026)

| Archivo | Descripción |
| :--- | :--- |
| `specs/005-catalog-and-hub/spec.md` | Actualizado para alinear contratos de primitivos sin namespace (`get`, `find`, `walk`) con operaciones reales y clarificar que el Hub no invoca a xolu (Eje 1: Qué se ofrece), delegando la ejecución a molu frontal. |
| `specs/006-mcp-server-stdio/spec.md` | Actualizado RF-3 y HU-2 con el mapeo exacto de los tres primitivos reales contra xolu (`GET /api/v1/{entity}/{id}`, `GET /api/v1/{entity}` / `POST /api/v1/oql/query`, `POST /api/v2/fsm/machine/{id}/walk` / `GET /api/v1/graph/...`) y mapeo a taxonomía tipada (`NOT_FOUND`, `EMPTY`, `INVALID_TRANSITION`). |
| `specs/006-mcp-server-stdio/plan.md` | Detallado `SubstrateExecutor`, enriquecido diagrama Mermaid con las 3 ramas de despacho a xolu y manejo de respuestas tipadas. |
| `specs/006-mcp-server-stdio/tasks.md` | T3 actualizada para validar contratos y ejecutar los tres primitivos reales de xolu (`get`, `find`, `walk`). |
| `pkg/mcp/server.go` | Implementación del servidor MCP: interfaces `PrimitiveProvider`, `HealthChecker`, `SubstrateExecutor`, loop JSON-RPC 2.0 sobre `stdio`, `HandleListTools` con formateo de herramientas sin namespace, fallback a `StandardPrimitives()`, `handleCallTool` con gating de salud `SUBSTRATE_UNAVAILABLE` (T3, RF-4), validación de existencia `NOT_OFFERED` (T3, RF-5), validación de contratos `CONTRACT_VIOLATION` con campos infractores (T4, RF-3), enrutamiento de ejecución y mapeo a taxonomía tipada (`NOT_FOUND`, `EMPTY`, `INVALID_TRANSITION` verbatim) (T5, RF-3), `ServeStdio` (RF-1, RF-2). En T6: `UpdatePrimitives()` con `sync.RWMutex` atómico, `SetOutput()`, `NotifyToolsListChanged()` emitiendo `notifications/tools/list_changed`, serialización protegida `writeMu` en `HandleMessage`, `NotifyToolsListChanged` y `Serve` (RF-6, RF-7, RF-8). |
| `pkg/mcp/primitives.go` | T2 (Spec 006): Definición de los 13 primitivos genéricos de xolu (§5.1: `describe`, `get`, `list`, `create`, `update`, `query`, `walk`, `machine_state`, `machine_history`, `cal_check`, `cal_openings`, `cal_propose`, `cal_confirm`) con JSON Schema Draft 2020-12, funciones `StandardPrimitives()`, `GetStandardPrimitive()`, `IsStandardPrimitive()` (RF-2, RF-8). |
| `pkg/mcp/primitives_test.go` | T2 (Spec 006): Tests unitarios verificando los 13 primitivos, tipos y propiedades de sus esquemas, ausencia estricta de namespace y neutralidad de dominio (RF-2, RF-8). |
| `pkg/mcp/server_test.go` | Tests unitarios para `initialize`, `tools/list` con esquemas, catálogo vacío, exposición de los 13 primitivos estándar (RF-1, RF-2); `tools/call` con `SUBSTRATE_UNAVAILABLE` (T3, RF-4), `NOT_OFFERED` (T3, RF-5), violaciones de contrato en tabla para múltiples primitivos `CONTRACT_VIOLATION` (T4, RF-3), tests T5 dedicados para entidades (`get` 404 → `NOT_FOUND`, `list` vacío → `EMPTY`, `create`, `update`), FSMs (`walk` rechazo de guarda → `INVALID_TRANSITION` verbatim, `machine_state` 404 → `NOT_FOUND`, `machine_history` vacío → `EMPTY`), consultas (`query` vacío → `EMPTY`), calendarios (`cal_check`, `cal_openings` vacío → `EMPTY`, `cal_propose`, `cal_confirm` 404 → `NOT_FOUND`) y `describe`. En T6: `TestServer_UpdatePrimitives_Notification` (notificación MCP `notifications/tools/list_changed`) y `TestServer_Concurrency` (concurrencia intensiva de lectura, llamada y actualización de herramientas bajo `-race`) (RF-6, RF-7, RF-8). |
| `pkg/mcp/server_e2e_test.go` | T7 (Spec 006): Test de integración punta a punta simulando el transporte `stdio` mediante `io.Pipe`, verificando ciclo de vida MCP completo (`initialize`, `tools/list` con los 13 primitivos estándar sin namespace, `tools/call` para múltiples primitivos contra `HTTPXoluClient` mockeado con mapeo a taxonomía tipada y hot-reload emitiendo `notifications/tools/list_changed`). (RF-1, RF-6). |
| `pkg/xolu/client.go` | T7 (Spec 006): Implementación de `Execute(ctx, primitive, args)` en `*HTTPXoluClient` satisfaciendo la interfaz `mcp.SubstrateExecutor` para los 13 primitivos genéricos (§5.1) hacia xolu REST/FSM/OQL/calendarios, con extracción de códigos de error de xolu o 404/500 hacia taxonomía y telemetría de latencia/métricas. |
| `pkg/xolu/client_test.go` | T7 (Spec 006): Tests unitarios para `Execute` con servidor HTTP de prueba simulando endpoints de xolu para entidades, FSM, consultas OQL, calendarios y mapeo de errores (`NOT_FOUND`, `INVALID_TRANSITION`, `SUBSTRATE_UNAVAILABLE`). |
| `pkg/xolu/client_mock.go` | T7 (Spec 006): Añadido `ExecuteFunc` e implementación de `Execute` en `MockXoluClient`. |
| `cmd/molu/main.go` | Conexión e instanciación de `mcp.NewServer()`, inyección de `Executor: xoluClient` (T7, RF-1), inicio de `ServeStdio(ctx)` en background, actualización de primitivos en `SIGHUP` emitiendo notificaciones `notifications/tools/list_changed` (RF-6), fallback de `loaderProvider` a `StandardPrimitives()` si xolu no devuelve primitivos. |
| `pkg/telemetry/logger.go` | Redirección de logs de consola/fallback de `os.Stdout` a `os.Stderr` para preservar el canal JSON-RPC puro en `stdio`. Protección con `sync.Once` de variables globales de `zerolog` para eliminar data races en pruebas paralelas. |
| `pkg/telemetry/middleware.go` | Generación de ULID con `crypto/rand.Reader` concurrente (eliminó data race de `ulid.DefaultEntropy`). |
| `pkg/schema/loader_test.go` & `client_test.go` | `MockSchemaClient` protegido con `sync.RWMutex` y método thread-safe `SetSchemas` (eliminó data race en `TestLoaderConcurrency`). |

---

## Spec 005 — T-005.4 «Ensamblar Hub con RPI y Transporte MCP» (sesión 23-oct-2026)

Plan en `specs/005-catalog-and-hub/tasks.md` (T1–T3). Dependencias cumplidas: `rpi.RPIProvider` (T-004.2) y `Registry`/`PublisherAPI` (T-005.2/3).

| Tarea | Estado | Archivos |
| :--- | :--- | :--- |
| **T1** `hub.Discovery` (catálogo → Admit → Score) | **Hecha** (`go test -race ./...` en verde) | `pkg/hub/discovery.go`, `pkg/hub/discovery_test.go` |
| **T2** `tools/list` de MCP consulta `hub.Discovery` | **Hecha** (`go test -race ./...` en verde) | `pkg/mcp/server.go`, `pkg/mcp/server_hub_test.go` |
| **T3** Wiring en `cmd/molu` + integración Criterio 7 | **Hecha** (`go test -race ./...` en verde; **T-005.4 completa**) | `pkg/config/config.go` (+test), `pkg/hub/rpi_provider.go` (+test), `pkg/hub_integration/tools_list_test.go`, `cmd/molu/main.go` |

`Discovery.ListTools(ctx, AdmitContext)`: tenant vacío → `CONTRACT_VIOLATION` (`fields: ["tenant"]`, sin consultar al proveedor, RF-9); nada admitido o catálogo vacío → `EMPTY` (RF-4); lo no admitido se omite sin rastro (RF-5); orden por score del proveedor (RF-6); errores del proveedor sin cambios.

**Decisiones confirmadas (23-oct-2026)**: `tenant`, `roles` y `query` llegan en los `params` de `tools/list` (spec 005), no por configuración; los primitivos de xolu no cambian en esta tarjeta. `docs/molu-part3-hub.md` dice «un hub por tenant»; si más adelante el tenant pasa a ser fijo por proceso, el cambio es local a T2/T3 y `Discovery` no se toca.

**T2 — contrato de `tools/list` con Hub**: `ServerConfig.Discovery` (interfaz `ToolDiscoverer`, la implementa `*hub.Discovery`). Con Discovery configurado, `tools/list` sirve el catálogo filtrado por RPI en orden de score; sin Discovery, el comportamiento anterior no cambia. `params` aceptan `tenant`, `roles`, `query` y el `cursor` estándar de MCP (ignorado); params mal formados → `CONTRACT_VIOLATION` (`fields: ["params"]`). Los errores tipados viajan completos en `error.data` (código y `details` intactos): `CONTRACT_VIOLATION` → JSON-RPC `-32602`, el resto de los tipados (p. ej. `EMPTY`) → `-32000`; un error no tipado → `-32603`.

**T3 — configuración y wiring**: `MOLU_HUB_RPI_PROVIDER` (`admit-all` | `static-context-rules`), `MOLU_HUB_RPI_RULES_FILE` y `MOLU_HUB_RPI_RULES_JSON` (excluyentes; el proveedor `static-context-rules` exige una de las dos; reglas sin proveedor o proveedor desconocido → error de configuración al arrancar). `hub.NewRPIProvider(name, rulesFile, rulesJSON)` construye el proveedor. **Sin `MOLU_HUB_RPI_PROVIDER` el descubrimiento queda desactivado** y `tools/list` sigue sirviendo los primitivos de xolu (desvío consciente de la spec 005, que propone `admit-all` por defecto, para no cambiar el comportamiento actual). Criterio 7 demostrado en `pkg/hub_integration`: el mismo catálogo y el mismo solicitante dan listas distintas solo por cambiar la config; también cubre `EMPTY` y `CONTRACT_VIOLATION` vía `tools/list` (Criterio 2).

**Pendiente tras T-005.4**: (1) el `Registry` de `cmd/molu` nace vacío y no hay camino MCP para publicar: `PublisherAPI` no está expuesta por ningún transporte (`tools/call` no existe aún), así que con el descubrimiento activado `tools/list` devuelve `EMPTY` hasta que se resuelva; (2) `tools/call` stub `NOT_OFFERED` (RF-7); (3) variables `MOLU_HUB_*` restantes de la spec 005 (transporte, puerto, auth, timeouts, logs); (4) diagrama `docs/diagrams/005-*.mmd`/`.pdf`; (5) documentar las variables nuevas en la guía de variables de entorno (Épica 9). Desbloquea T-007.2, T-008.2 y T-008.3.

---

## Hallazgo: `schema-reader` (Spec 003) no coincide con xolu real (verificado 23-oct-2026)

Probado con xolu **v0.30.44** (la más reciente; `Downloads/xolu-main`, compilada y levantada en local) y `bin/molu`:
- `GET /ready` → `{"status":"ready"}` (compatible con la sonda).
- `GET /api/v1/schemas` → `{"count":0,"schemas":[]}`: solo la lista de tipos de entidad con schema registrado (`[{"name":"asset"}, ...]`). `pkg/schema` espera `{"primitives":[...],"stateMachines":[...],"payloadTypes":[...]}`.
- Consecuencia: contra xolu real molu arranca sin error, pero `tools/list` (sin Hub) devuelve `[]` en vez de `walk`/`find`/`get`. Las pruebas con xolu falso lo ocultaban.
- Contexto: `docs/molu-part2-mcp-frontend.md` describe un «mapa semántico» armado desde varias rutas de xolu (schemas de entidades, FSM, generadores, eventos) y 13 herramientas fijas; el spec 003 (endpoint único con `primitives`) parece un diseño anterior que el spec 006 reemplaza. **Por confirmar con el equipo** (tarjeta «Ejecución de Primitivos», en progreso). No se tocó `pkg/schema`.
- Slack (canal del equipo, 19-oct): se eligió la propuesta A (capa externa del servidor MCP exponiendo primero las funciones nativas de xolu); las funciones de Seam se integran después.

