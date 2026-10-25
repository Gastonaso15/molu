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

- [x] **T6. Refresco dinámico de herramientas y verificación de concurrencia.** RF-6, RF-7, RF-8
  - Implementar `UpdatePrimitives(newPrimitives []schema.PrimitiveSchema)` con `sync.RWMutex` para reemplazo atómico y emisión de notificación `notifications/tools/list_changed`.
  - Implementar test de concurrencia intensiva `TestServer_Concurrency` ejecutando `ListTools`, `CallTool` y `UpdatePrimitives` en paralelo bajo `-race`.
  - Hecho cuando: `go test -v -race ./pkg/mcp/...` pase sin advertencias ni condiciones de carrera.

- [x] **T7. Cableado en `cmd/molu/main.go` sobre transporte stdio y verificación punta a punta.** RF-1, RF-6
  - Configurar `mcp.NewServer()` conectando `SchemaLoader`, `Probe` y el ejecutor HTTP de xolu.
  - Asegurar aislamiento estricto de `os.Stdout` (redirección total de telemetría/logs a `os.Stderr`).
  - Conectar señal `SIGHUP` para hot-reload.
  - Hecho cuando: `make build` compile limpiamente y `go test -v -race ./...` pase en verde.

- [ ] **T8. Diagrama de secuencia y documentación operativa.** RF-1 a RF-8
  - Actualizar `docs/diagrams/006-mcp-server-stdio-sequence.mmd` con el flujo completo de los 13 primitivos.
  - Actualizar `MEMORY.md` y `README.md`.
  - Hecho cuando: Diagrama y documentación reflejen el estado real del servidor MCP.

## Correcciones de `tools/call` contra xolu real (verificadas el 24-oct-2026 con xolu v0.30.44)

Contexto: los tests de T5 usaban servidores falsos con un formato de error plano (`{"code":...}`) y rutas inventadas. Contra xolu real, que responde `{"error":{"code","message","status"}}`, varios primitivos fallan. Para FSM y calendario xolu requiere `XOLU_API_V2_ENABLED=true` y `XOLU_CAL_ENABLED=true`.

- [x] **T9.0. Rutas de calendario `/api/v2/cal/*`.** RF-3
  - `cal_check`, `cal_openings`, `cal_propose` y `cal_confirm` apuntaban a `/api/v1/cal/*`. Corregido en `pkg/xolu/client.go`, su test, `spec.md` y `plan.md`.

- [x] **T9.1. Sobre de error anidado de xolu y `NOT_FOUND` en FSM.** RF-3
  - `doRequest` entiende `{"error":{"code","message","status"}}`: un 404 se expone como `*xolu.HTTPError` (con `HTTPStatus()`), y otros errores con `code` salen como `*taxonomy.TypedError` con el código de xolu verbatim.
  - `handleCallTool` mapea 404 a `NOT_FOUND` también para `walk` (`machine_id`) y `machine_history`, detectando el estado HTTP y no solo el texto del mensaje.
  - Hecho cuando: `go test -v -race ./pkg/xolu/... ./pkg/mcp/...` pasa con los casos nuevos, y contra xolu real `walk` y `machine_history` sobre una máquina inexistente devuelven `NOT_FOUND`.

- [x] **T9.2. `EMPTY` para `list` con la clave `data`.** RF-3
  - `isEmptyCollection` debe reconocer `{"data":[],"pagination":{...}}` (clave `data`, y `pagination.total_items == 0`).
  - Hecho cuando: test unitario cubre `list` vacío con el formato real de xolu, y `list` sobre una entidad sin registros devuelve `EMPTY`.

- [ ] **T9.3. Contrato de `cal_check` (y revisar `cal_openings`, `cal_propose`, `cal_confirm`).** RF-3
  - El cuerpo de molu usa `calendars[]`, xolu espera `calendar_id`. Verificar contra `pkg/server/v2_cal_handlers.go` de xolu y alinear schema y cuerpo.
  - Hecho cuando: `cal_check` contra xolu real con un calendario existente no devuelve 400.

- [ ] **T9.4. Decisión pendiente: contenido de `describe`.** RF-3
  - Hoy devuelve `/api/v1/schemas` (formato que no coincide con `pkg/schema`). Requiere decidir si debe devolver el mapa semántico de la Parte 2.

- [x] **T9.5. `list` honra `limit`, `offset` y `filter`.** RF-3
  - xolu pagina con `page`/`per_page` (máximo 100; por encima ignora el valor y usa 10) y trata cualquier otro parámetro de consulta como filtro de igualdad, así que `limit=2` devolvía una lista vacía o la página por defecto. `HTTPXoluClient` traduce `limit` (por defecto 50) y `offset` a las páginas que cubren la ventana y la recorta exacta; `filter` pasa como filtros de igualdad; las claves `page` y `per_page` en `filter` devuelven `CONTRACT_VIOLATION`. La respuesta conserva `data` y `pagination` (`total_items`, `offset`, `limit`).
  - Verificado contra xolu real (16 activos): `limit 2`, `limit 2 offset 2`, `offset` no alineado, `filter`, ventana más allá del final (`EMPTY`) y entidad vacía (`EMPTY`). Un agente (Claude Code en modo no interactivo) lo usó por MCP y obtuvo los 3 activos y el total.

- [ ] **T9.6. Decisión pendiente: cómo ve el agente los errores tipados.** RF-3
  - Hoy los errores tipados viajan como error JSON-RPC (`error.data.code`). Probado con un agente real: Claude Code solo ve el mensaje de texto, no el código `XOLU-MOLU-FRONT-NOT_FOUND`/`EMPTY`, y el mensaje incluye `tenant ""`, que confunde. En MCP, los fallos de herramienta suelen devolverse como resultado con `isError: true` y el código dentro del texto. Requiere decidir con el equipo si se cambia el transporte del error, porque afecta la taxonomía tipada (mandato 1 de `AGENTS.md`).
