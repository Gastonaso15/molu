# Tareas: Spec 009 — Hub Standalone y Publicador Piloto de Work Orders

## Resumen de Fases
- **Fase 1 (T1–T4)**: Hub Standalone (`bin/hub`), endpoints HTTP de registro/discovery y cliente remoto en `molu`.
- **Fase 2 (T5–T8)**: Servicio de dominio Work Orders (`bin/publisher-orders`), integración FSM xolu, servidor HTTP y auto-registro.
- **Fase 3 (T9)**: Integración E2E completa y verificación del Criterio 1 (rechazo de FSM visible verbatim).

---

## Tareas de Implementación

- [x] **T1. Servidor HTTP del Hub (`pkg/hub/http_server.go`).** RF-2, RF-3, RF-4, RF-13, RF-14
  - **Archivos**: `pkg/hub/http_server.go`, `pkg/hub/http_server_test.go`
  - **Descripción**: Implementar `HubHTTPServer` exponiendo:
    - `POST /api/v1/registry/register` $\rightarrow$ valida contrato JSON y registra en `Registry`. Retorna 200 OK o 400 `CONTRACT_VIOLATION`.
    - `POST /api/v1/registry/unregister` $\rightarrow$ desregistra o retorna 404 `NOT_FOUND`.
    - `POST /api/v1/registry/list` $\rightarrow$ lista herramientas por `publisher_id` o catálogo completo.
    - `POST /api/v1/discovery/tools` $\rightarrow$ ejecuta `discovery.ListTools` con `rpi.AdmitContext`. Retorna 400 si tenant está vacío, 200 con `EMPTY` si catálogo vacío, o 200 con herramientas ordenadas por score.
    - `GET /healthz` $\rightarrow$ 200 OK.
  - **Criterio de finalización**: `go test -v -race ./pkg/hub/... -run "TestHubHTTPServer"` pasa con cobertura total de endpoints y manejo de errores tipados.

- [x] **T2. Binario del Hub Standalone (`cmd/hub/main.go`).** RF-1, RF-13, RF-14
  - **Archivos**: `cmd/hub/main.go`, `cmd/hub/main_test.go`
  - **Descripción**: Crear el punto de entrada para `bin/hub`. Lee variables `MOLU_HUB_HTTP_ADDR` (default `:8091`), `MOLU_HUB_RPI_PROVIDER`, etc., inicializa registro y logger, arranca `HubHTTPServer` y soporta graceful shutdown ante `SIGINT`/`SIGTERM`.
  - **Criterio de finalización**: Compila limpiamente con `go build -o bin/hub ./cmd/hub` y el test verifica arranque y apagado ordenado.

- [x] **T3. Cliente Remoto de Hub en Molu (`pkg/hub/client.go`).** RF-5, RF-13, RF-14
  - **Archivos**: `pkg/hub/client.go`, `pkg/hub/client_test.go`
  - **Descripción**: Implementar `RemoteHubClient` que implementa `mcp.CatalogLookup` (`Lookup`) y `mcp.ToolDiscoverer` (`ListTools`) mediante llamadas HTTP POST a `/api/v1/registry/list` y `/api/v1/discovery/tools`. Deserializa y propaga errores de taxonomía tipada (`TypedError`) sin colapsarlos.
  - **Criterio de finalización**: `go test -v -race ./pkg/hub/... -run "TestRemoteHubClient"` pasa con simulación de servidor HTTP.

- [x] **T4. Wiring de Hub Remoto en Molu Frontend (`cmd/molu/main.go` & `pkg/config/config.go`).** RF-5
  - **Archivos**: `pkg/config/config.go`, `pkg/config/config_test.go`, `cmd/molu/main.go`
  - **Descripción**: Agregar variable de configuración `MOLU_FRONT_HUB_URL`. En `cmd/molu/main.go`, si la variable está definida, instanciar `RemoteHubClient` para `CatalogLookup` y `Discovery`; si está vacía, mantener el `Registry` local en memoria para retrocompatibilidad y tests.
  - **Criterio de finalización**: `go test -v -race ./pkg/config/...` y `go test -v -race ./cmd/molu/...` pasan.

- [x] **T5. Dominio y Operaciones de Work Orders (`pkg/publisher/orders/service.go`).** RF-7, RF-8, RF-9, RF-11
  - **Archivos**: `pkg/publisher/orders/types.go`, `pkg/publisher/orders/service.go`, `pkg/publisher/orders/service_test.go`
  - **Descripción**: Definir tipos `WorkOrder` y servicio que interactúa con xolu (`/api/v1/orders`, `/api/v2/fsm/walk`). Implementar `CreateOrder`, `GetOrder` (404 ante inexistencia) y `AdvanceOrder` (invoca `walk` en FSM; si xolu rechaza la transición, retorna error `XOLU-INVALID_TRANSITION` verbatim).
  - **Criterio de finalización**: `go test -v -race ./pkg/publisher/orders/... -run "TestOrderService"` pasa contra mock de xolu.

- [x] **T6. Servidor HTTP y Middleware de Autenticación (`pkg/publisher/orders/server.go`).** RF-7, RF-8, RF-9, RF-10, RF-11
  - **Archivos**: `pkg/publisher/orders/server.go`, `pkg/publisher/orders/server_test.go`
  - **Descripción**: Servidor HTTP en `:8092` con endpoints `POST /api/v1/orders/create`, `POST /api/v1/orders/advance`, `POST /api/v1/orders/get` y `GET /healthz`. Middleware Bearer valida cabecera `Authorization: Bearer <token>`. Retorna HTTP 401 si token inválido, 404 si orden no existe, 409 con sobre xolu si transición rechazada, y 503 si xolu inalcanzable.
  - **Criterio de finalización**: `go test -v -race ./pkg/publisher/orders/... -run "TestOrderServer"` pasa.

- [x] **T7. Auto-registro en el Hub y Ciclo de Vida (`pkg/publisher/orders/registrar.go`).** RF-6, RF-12
  - **Archivos**: `pkg/publisher/orders/registrar.go`, `pkg/publisher/orders/registrar_test.go`
  - **Descripción**: Componente que registra las 3 herramientas (`work_orders.CreateOrder`, `work_orders.AdvanceOrder`, `work_orders.GetOrder`) en el Hub remoto con sus esquemas JSON y metadatos `token_ref: "orders:api-token"`. Incluye reintentos con backoff al arranque y desregistro ante shutdown.
  - **Criterio de finalización**: `go test -v -race ./pkg/publisher/orders/... -run "TestOrderRegistrar"` pasa.

- [x] **T8. Binario del Publicador y Makefile (`cmd/publisher-orders/main.go` & `makefile`).** RF-6, RF-13, RF-14
  - **Archivos**: `cmd/publisher-orders/main.go`, `makefile`
  - **Descripción**: Binario del publicador leyendo `MOLU_PUBLISHER_*`. Actualizar `makefile` para que `make build` compile `bin/molu`, `bin/hub` y `bin/publisher-orders`.
  - **Criterio de finalización**: `make build` compila los 3 binarios sin errores y `grep -ri seam cmd/ pkg/` da cero coincidencias.

- [x] **T9. Prueba de Integración E2E y Validación del Criterio 1.** RF-1 a RF-14, Criterio 1
  - **Archivos**: `pkg/bridge_integration/criterio1_e2e_test.go`
  - **Descripción**: Prueba integral donde un cliente MCP consulta tools via stdio a Molu $\rightarrow$ Molu consulta Hub remoto desacoplado $\rightarrow$ Publisher auto-registrado atiende llamadas de dominio despachadas por el Execution Bridge $\rightarrow$ xolu rechaza transición FSM inválida $\rightarrow$ se comprueba que el error `INVALID_TRANSITION` con el mensaje exacto de la guarda llega intacto al cliente MCP.
  - **Criterio de finalización**: `go test -v -race ./pkg/bridge_integration/... -run "TestCriterio1_E2E"` pasa exitosamente.

