# Tareas: Spec 011 — Cierre de Brechas de Auditoría y Entregables Normativos

## Resumen de Bloques
- **T1**: Interfaz RPI (`Capabilities`), `ScoreWithLogging` en Discovery, test double de solo-puntuación y nota de extensibilidad.
- **T2–T3**: Hub Standalone (protección contra colisión de primitivos reservados, autorización de publicadores por namespace §2.6 y telemetría completa).
- **T4–T6**: Molu Frontend (caché y sondeo periódico del Hub, superficie unificada en `tools/list`, transporte `streamable-http` completo).
- **T7–T10**: Documentación normativa §2.3.1 (despliegue, configuración, runbook), diagramas `.mmd` + `.pdf`, ADRs y validación integral.

---

## Lista de Tareas (TDD estricto)

- [x] **T1. Capacidades RPI (`Capabilities`), `ScoreWithLogging` y extensibilidad de `Score`.** RF-6, RF-7, RF-8
  - **Archivos**: `pkg/rpi/admit_score.go`, `pkg/rpi/providers/admit_all.go`, `pkg/rpi/providers/static_context_rules.go`, `pkg/rpi/provider_mock.go`, `pkg/rpi/score_double_test.go`, `pkg/hub/discovery.go`, `docs/rpi-extensibility.md`
  - **Hecho cuando**: `go test -v -race ./pkg/rpi/... ./pkg/hub/...` pasa verificando `Capabilities()`, `ScoreWithLogging` y todas las garantías de §2.5 sobre `ScoringOnlyDouble`, y existe `docs/rpi-extensibility.md`.

- [x] **T2. Protección contra colisión con nombres reservados de primitivos en `hub.Registry`.** RF-5
  - **Archivos**: `pkg/hub/registry.go`, `pkg/hub/registry_test.go`
  - **Hecho cuando**: `go test -v -race ./pkg/hub/... -run "TestRegistry"` pasa demostrando que registrar una herramienta que colisiona con los nombres reservados de primitivos genéricos retorna `CONTRACT_VIOLATION` con `details.fields: ["name"]`.

- [x] **T3. Autorización de publicadores por namespace (§2.6) y telemetría en Hub Standalone.** RF-4, RF-14
  - **Archivos**: `pkg/hub/auth.go`, `pkg/hub/auth_test.go`, `pkg/hub/http_server.go`, `pkg/hub/http_server_test.go`, `cmd/hub/main.go`, `cmd/hub/main_test.go`
  - **Hecho cuando**: `go test -v -race ./pkg/hub/... ./cmd/hub/...` pasa cubriendo mapa `token -> []namespace` (401, 403 `NOT_OFFERED`, 200 autorizado, modo abierto), correlación `X-Request-ID`/`traceparent`, logs estructurados y métricas en `cmd/hub`.

- [x] **T4. Caché thread-safe y sondeo periódico con retención en `RemoteHubClient`.** RF-2
  - **Archivos**: `pkg/hub/client.go`, `pkg/hub/client_test.go`
  - **Hecho cuando**: `go test -v -race ./pkg/hub/... -run "TestRemoteHubClient"` pasa verificando `StartPolling(ctx, interval)`, retención del último catálogo válido ante caída transitoria del Hub y `SUBSTRATE_UNAVAILABLE` si nunca hubo catálogo.

- [x] **T5. Superficie unificada en `tools/list` en `pkg/mcp/server.go`.** RF-1
  - **Archivos**: `pkg/mcp/server.go`, `pkg/mcp/server_hub_test.go`, `pkg/hub_integration/tools_list_test.go`
  - **Hecho cuando**: `go test -v -race ./pkg/mcp/... ./pkg/hub_integration/... ./pkg/bridge_integration/...` pasa verificando que `tools/list` con Hub conectado devuelve los 13 primitivos genéricos + funciones admitidas del Hub, fallback a primitivos ante `EMPTY`, `CONTRACT_VIOLATION` sin tenant ni `DefaultTenant`, y precedencia del primitivo ante colisión.

- [ ] **T6. Transporte `streamable-http` completo y wiring en `cmd/molu/main.go`.** RF-2, RF-3
  - **Archivos**: `pkg/mcp/http_transport.go`, `pkg/mcp/http_transport_test.go`, `cmd/molu/main.go`
  - **Hecho cuando**: `go test -v -race ./pkg/mcp/... -run "TestHTTPTransport"` pasa cubriendo POST JSON, POST SSE (`Accept: text/event-stream`), GET SSE (`notifications/tools/list_changed`), auth `none`/`bearer`, y `cmd/molu` selecciona `stdio` vs `streamable-http` e inicia `remoteHub.StartPolling`.

- [ ] **T7. Guía de despliegue de 4 procesos y sincronización de `docs/configuration.md`.** RF-9, RF-10
  - **Archivos**: `docs/deployment.md`, `docs/configuration.md`
  - **Hecho cuando**: `docs/deployment.md` cubre los 4 procesos (`xolu`, `hub`, `publisher-orders`, `molu`), orden tolerado y verificación de conectividad; `docs/configuration.md` coincide 1:1 con `pkg/config/config.go` y las variables de `cmd/hub` y `cmd/publisher-*`.

- [ ] **T8. Ampliación de `docs/runbook.md` (los 6 códigos de taxonomía y catálogo del Hub).** RF-11
  - **Archivos**: `docs/runbook.md`
  - **Hecho cuando**: `docs/runbook.md` documenta el procedimiento operativo para los 6 códigos de §2.4 + `XOLU-MOLU-FRONT-TIMEOUT`, retención de catálogo del Hub, y no contiene enlaces rotos.

- [ ] **T9. Diagramas de secuencia propios en `.mmd` y renderizado a `.pdf` (`-b white`).** RF-12
  - **Archivos**: `docs/diagrams/006-mcp-server-sequence.mmd`, `docs/diagrams/008-execution-bridge-sequence.mmd`, `docs/diagrams/009-work-orders-create.mmd`, `docs/diagrams/009-work-orders-get.mmd`, `docs/diagrams/009-work-orders-advance.mmd`, `docs/diagrams/009-work-orders-list-templates.mmd`, y los 12 archivos `docs/diagrams/*.pdf`
  - **Hecho cuando**: Existen los 12 `.mmd` y sus 12 `.pdf` renderizados con tamaño > 0 bytes y fondo blanco (`-b white`).

- [ ] **T10. ADRs 013 y 014, cierre de T8 en Specs 006/008 y validación integral final.** RF-13, RF-1 a RF-14
  - **Archivos**: `MEMORY.md`, `specs/006-mcp-server-stdio/tasks.md`, `specs/008-execution-bridge/tasks.md`, `specs/011-deliverables-closure/spec.md`
  - **Hecho cuando**: `go test -v -race ./...`, `go vet ./...`, `make build` y chequeo de neutralidad pasan al 100%, y `MEMORY.md` refleja el estado final cerrado.
