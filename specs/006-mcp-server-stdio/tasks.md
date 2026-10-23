# Tareas: Spec 006 — Servidor MCP Stdio y Despachador de Primitivos en Molu Frontend

- [x] **T1. Estructura del Servidor MCP y descubrimiento inicial de herramientas (`tools/list`).** RF-1, RF-2
  - Definir interfaces `PrimitiveProvider`, `HealthChecker` y `SubstrateExecutor` en `pkg/mcp/server.go`.
  - Implementar constructor `NewServer(cfg ServerConfig)` y método `HandleListTools(ctx context.Context)`.
  - Hecho cuando: `pkg/mcp/server_test.go` verifique con `go test -v -race ./pkg/mcp/...` que `tools/list` expone los primitivos con formato MCP correcto sin namespace, o lista vacía si no hay esquemas.

- [x] **T2. Definición estandarizada de los 13 primitivos genéricos (§5.1) y exposición completa en `tools/list`.** RF-2, RF-8
  - Definir en `pkg/mcp/primitives.go` los schemas JSON Draft 2020-12 completos para los 13 primitivos: `describe`, `get`, `list`, `create`, `update`, `query`, `walk`, `machine_state`, `machine_history`, `cal_check`, `cal_openings`, `cal_propose`, `cal_confirm`.
  - Asegurar que `tools/list` reporte los 13 primitivos sin namespace con descripciones y tipos de entrada exactos.
  - Hecho cuando: Tests unitarios en `server_test.go` verifiquen la presencia y estructura de los 13 primitivos.

- [x] **T3. Despacho de herramientas (`tools/call`): Gating de salud y herramientas no ofrecidas.** RF-4, RF-5
  - Implementar en `server.go` la comprobación previa de salud con `HealthChecker.Check()`; si xolu está caído, retornar `SUBSTRATE_UNAVAILABLE` con `RetryMetadata`.
  - Si la herramienta solicitada no existe entre los 13 primitivos conocidos, retornar `NOT_OFFERED`.
  - Hecho cuando: Tests dedicados en `server_test.go` pasen en verde con `go test -v -race ./pkg/mcp/...`.

- [x] **T4. Validación de contratos en `tools/call` para los 13 primitivos.** RF-3
  - Integrar `PrimitiveProvider.ValidateInput(toolName, args)`; si la entrada viola el schema (campos obligatorios ausentes, formatos de fecha inválidos, etc.), retornar `CONTRACT_VIOLATION` con `details.fields[]`.
  - Hecho cuando: Tests unitarios cubran violaciones de schema para `create`, `update`, `cal_check`, `get`, `walk`, etc., pasando en verde con `go test -v -race ./pkg/mcp/...`.

- [x] **T5. Ejecución y despacho de los 13 primitivos hacia `SubstrateExecutor` con mapeo de errores de taxonomía.** RF-3
  - Implementar enrutamiento en `server.go` hacia `SubstrateExecutor.Execute(ctx, toolName, args)` para los 13 primitivos:
    - Entidades: `get` (mapeo 404 a `NOT_FOUND`), `list` (mapeo vacío a `EMPTY`), `create`, `update`.
    - FSMs: `walk` (mapeo rechazo guarda a `INVALID_TRANSITION` verbatim), `machine_state` (404 a `NOT_FOUND`), `machine_history` (vacío a `EMPTY`).
    - Consultas: `query` (vacío a `EMPTY`).
    - Calendarios: `cal_check`, `cal_openings` (vacío a `EMPTY`), `cal_propose`, `cal_confirm` (404 a `NOT_FOUND`).
    - Orientación: `describe` (retorno de esquema/mapa según `scope`).
  - Hecho cuando: Tests unitarios y de integración de cada primitivo y sus códigos de taxonomía pasen en verde con `go test -v -race ./...`.

- [ ] **T6. Refresco dinámico de herramientas y verificación de concurrencia.** RF-6, RF-7, RF-8
  - Implementar `UpdatePrimitives(newPrimitives []schema.PrimitiveSchema)` con `sync.RWMutex` para reemplazo atómico y emisión de notificación `notifications/tools/list_changed`.
  - Implementar test de concurrencia intensiva `TestServer_Concurrency` ejecutando `ListTools`, `CallTool` y `UpdatePrimitives` en paralelo bajo `-race`.
  - Hecho cuando: `go test -v -race ./pkg/mcp/...` pase sin advertencias ni condiciones de carrera.

- [ ] **T7. Cableado en `cmd/molu/main.go` sobre transporte stdio y verificación punta a punta.** RF-1, RF-6
  - Configurar `mcp.NewServer()` conectando `SchemaLoader`, `Probe` y el ejecutor HTTP de xolu.
  - Asegurar aislamiento estricto de `os.Stdout` (redirección total de telemetría/logs a `os.Stderr`).
  - Conectar señal `SIGHUP` para hot-reload.
  - Hecho cuando: `make build` compile limpiamente y `go test -v -race ./...` pase en verde.

- [ ] **T8. Diagrama de secuencia y documentación operativa.** RF-1 a RF-8
  - Actualizar `docs/diagrams/006-mcp-server-stdio-sequence.mmd` con el flujo completo de los 13 primitivos.
  - Actualizar `MEMORY.md` y `README.md`.
  - Hecho cuando: Diagrama y documentación reflejen el estado real del servidor MCP.
