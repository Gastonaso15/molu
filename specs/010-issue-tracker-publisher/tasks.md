# Tareas: Spec 010 — Publicador de Issue Tracker

## Resumen de Tareas
- [x] **T1. Dominio y Operaciones de Issue Tracker (`pkg/publisher/issues/service.go`).** RF-1, RF-2, RF-3, RF-4, RF-7, RF-9, RF-10
  - **Archivos**: `pkg/publisher/issues/types.go`, `pkg/publisher/issues/service.go`, `pkg/publisher/issues/service_test.go`
  - **Descripción**: Definir tipos `Issue`, matriz y cálculo de SLA (primera respuesta y resolución), y servicio que interactúa con xolu (`/api/v1/issues`, `/api/v1/assets`, `/api/v2/fsm/walk`). Implementar `CreateIssue` (calcula SLA, crea máquina FSM `issue_lifecycle` y entidad `issues`), `GetIssue` (404 ante inexistencia) y `AdvanceIssue` (invoca `walk` en FSM; si xolu rechaza la transición, retorna error `XOLU-INVALID_TRANSITION` verbatim).
  - **Criterio de finalización**: `go test -v -race ./pkg/publisher/issues/... -run "TestIssueService"` pasa contra mock de xolu.

- [x] **T2. Servidor HTTP y Middleware de Autenticación (`pkg/publisher/issues/server.go`).** RF-1, RF-2, RF-3, RF-6, RF-7
  - **Archivos**: `pkg/publisher/issues/server.go`, `pkg/publisher/issues/server_test.go`
  - **Descripción**: Servidor HTTP en `:8093` con endpoints `POST /api/v1/issues/create`, `POST /api/v1/issues/advance`, `POST /api/v1/issues/get` y `GET /healthz`. Middleware Bearer valida cabecera `Authorization: Bearer <token>`. Retorna HTTP 401 si token inválido, 404 si incidencia no existe, 409 con sobre xolu si transición rechazada, y 503 si xolu inalcanzable.
  - **Criterio de finalización**: `go test -v -race ./pkg/publisher/issues/... -run "TestIssueServer"` pasa.

- [ ] **T3. Auto-registro en el Hub y Ciclo de Vida (`pkg/publisher/issues/registrar.go`).** RF-5, RF-8
  - **Archivos**: `pkg/publisher/issues/registrar.go`, `pkg/publisher/issues/registrar_test.go`
  - **Descripción**: Componente que registra las 3 herramientas (`issues.CreateIssue`, `issues.AdvanceIssue`, `issues.GetIssue`) en el Hub remoto con sus esquemas JSON y metadatos `token_ref: "issues:api-token"`. Incluye reintentos con backoff al arranque y desregistro ante shutdown.
  - **Criterio de finalización**: `go test -v -race ./pkg/publisher/issues/... -run "TestIssueRegistrar"` pasa.

- [ ] **T4. Binario del Publicador y Makefile (`cmd/publisher-issues/main.go` & `makefile`).** RF-5, RF-9, RF-10
  - **Archivos**: `cmd/publisher-issues/main.go`, `makefile`
  - **Descripción**: Binario del publicador leyendo `MOLU_PUBLISHER_ISSUES_*`. Actualizar `makefile` para que `make build` compile `bin/molu`, `bin/hub`, `bin/publisher-orders` y `bin/publisher-issues`.
  - **Criterio de finalización**: `make build` compila los 4 binarios sin errores y `grep -ri seam cmd/ pkg/` da cero coincidencias.

- [ ] **T5. Prueba de Integración E2E para Issue Tracker (`pkg/bridge_integration/issues_e2e_test.go`).** RF-1 a RF-10
  - **Archivos**: `pkg/bridge_integration/issues_e2e_test.go`
  - **Descripción**: Prueba integral donde un cliente MCP consulta tools via stdio a Molu $\rightarrow$ Molu consulta Hub remoto $\rightarrow$ Publisher de issues auto-registrado atiende llamadas de dominio despachadas por el Execution Bridge $\rightarrow$ se verifica la creación con SLA y el rechazo verbatim de transición inválida.
  - **Criterio de finalización**: `go test -v -race ./pkg/bridge_integration/... -run "TestIssues_E2E"` pasa exitosamente.

