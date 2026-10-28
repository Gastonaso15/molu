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

- [x] **T9.3. Contrato de `cal_check`, `cal_openings`, `cal_propose` y `cal_confirm`.** RF-3
  - Implementado en `pkg/xolu/calendar.go` (`executeCalendar`); antes estaba dentro de `Execute` y sin tests. xolu trabaja con **un calendario por petición** y no busca reservas por id. Decisiones:
    - `cal_confirm` **no tiene calendario por defecto** (antes `"VH-001"`): `calendar_id` pasa a ser obligatorio en el esquema del primitivo (se admite también `calendars` con un solo elemento). Sin él, `CONTRACT_VIOLATION` sin llamar a xolu. `cal_propose` ya devuelve `calendar_id` para encadenarlo. **Esto se aparta del §5.1.13 del diseño, que pedía solo `booking_id`.**
    - `duration` de `cal_openings` se lee como ISO 8601 (`PT30M`, `PT1H30M`, `P1D`, `P1DT2H`; también `30m` y `duration_ms`). Ilegible o ausente → `CONTRACT_VIOLATION` sobre `duration`, no una hora en silencio.
    - Varios calendarios: `cal_check` pregunta a cada uno, intersecta (`feasible`) y nombra los bloqueantes (`blocking_calendars`, más la respuesta de cada calendario). Con uno solo devuelve la respuesta de xolu tal cual. `cal_openings` y `cal_propose` rechazan más de uno con `CONTRACT_VIOLATION` sobre `calendars` en lugar de ignorar los demás (xolu no intersecta ventanas ni reserva en varios calendarios).
    - Calendario inexistente (404 de xolu) en `cal_check`, `cal_openings` y `cal_propose` → `NOT_FOUND` nombrando el calendario (`pkg/mcp/server.go`).
  - Tests: `pkg/xolu/calendar_test.go` (11) y 2 en `pkg/mcp/server_test.go`, escritos primero en rojo; se actualizaron 3 tests viejos que usaban el comportamiento anterior. `go test -race ./...` en verde.
  - Verificado por MCP contra xolu real v0.30.44: `cal_openings` con `PT30M`, `cal_check` con dos calendarios, calendario inexistente (`NOT_FOUND`), y la secuencia `cal_propose` → `cal_confirm` con el `calendar_id` devuelto; `cal_confirm` sin calendario y con una reserva inexistente.
  - **No resuelto:** (a) `cal_propose` sigue inyectando `bearer: 1` si no se indica; xolu solo exige un bearer distinto de 0 para confirmar, así que `1` es un valor arbitrario de la demo. (b) Un 500 de xolu sigue informándose como `SUBSTRATE_UNAVAILABLE`; confirmar sin bearer devuelve ese 500 (`XOLU-ST006: binding requires a live bearer`). (c) `default`/`examples` de `primitives.go` siguen con valores de la demo (`VH-001`, `bk-1001`); tarea aparte. (d) Con `calendar_id` ausente el validador nombra el campo como `(root)` (bug conocido).

- [x] **T9.4. `describe` devuelve el mapa semántico de la Parte 2 (§4) con `scope` y `name`.** RF-3
  - Antes devolvía `/api/v1/schemas`, que en xolu solo lista las entidades con esquema registrado (ninguna con las semillas) y no coincide con `pkg/schema`. Ahora `HTTPXoluClient.Execute` lo sirve con `executeDescribe` (`pkg/xolu/describe.go`), que lee xolu en vivo:
    - `entities`: `GET /api/v1/entities` (tipo, filas, `has_schema`) y, solo para los que tienen esquema, `GET /api/v1/schema/{entity}` incrustado en `schema`.
    - `machines`: `GET /api/v2/fsm/def` y `GET /api/v2/fsm/def/{id}` (`spec` completo).
    - `generators`: `GET /api/v2/gen/seq`. `events`: `GET /api/v2/event/def`.
  - `scope` por defecto `all`. En `all`, un grupo que xolu no ofrece (404, p. ej. `/api/v2` apagado) se informa en `unavailable` sin fallar; en un `scope` explícito, o ante cualquier otro error (p. ej. 5xx → `SUBSTRATE_UNAVAILABLE`), el error se propaga. `scope` desconocido → `CONTRACT_VIOLATION`.
  - `name` filtra sin distinguir mayúsculas por `entity_type`, `name` o `id`, y solo pide el detalle de lo que coincide. Sin coincidencias en ningún grupo → `NOT_FOUND`.
  - Tests: `pkg/xolu/describe_test.go` (11) más el ajuste del falso xolu de `TestHTTPXoluClient_Execute_CalendarsAndDescribe`. `go test -race ./...` en verde. Verificado por MCP contra xolu real v0.30.44 (20 entidades, 2 máquinas; `name`, `NOT_FOUND`, `CONTRACT_VIOLATION`, y un esquema real incrustado tras promover `regions`).
  - Límite conocido: las semillas no registran esquemas, así que hoy las 20 entidades salen con `has_schema: false` y sin lista de campos (ver T9.7 para cómo se cubre). xolu rechaza registrar un esquema si la entidad ya tiene filas (409 `XOLU-ST009`); hay que registrarlo antes de cargar datos o promover con `POST /api/v1/entities/promote/strict/{entity}` enviando el esquema en el cuerpo.

- [x] **T9.7. `describe` incluye el esquema inferido por xolu cuando la entidad no tiene uno.** RF-3
  - xolu infiere un esquema desde las filas con `GET /api/v1/entity/{type}/schema-suggestion` (solo lectura, solo con almacenamiento SQLite). Con `name`, cada entidad sin esquema registrado lleva `suggestion`: `{inferred: true, sampled_rows, total_rows, suggested_schema, field_analysis}`. Nunca se presenta como `schema`: es una conjetura de los datos, no un contrato (p. ej. un `enum` sale de los valores vistos, un campo presente en pocas filas sale opcional).
  - Solo se pide cuando un `name` acota la respuesta: es una petición por entidad y `scope: all` sin `name` serían 20.
  - Si xolu no puede inferirlo (entidad sin filas, otro almacenamiento, 4xx) la entidad lleva `suggestion_unavailable` con el motivo y `describe` no falla; un fallo del sustrato (5xx) sí se propaga como `SUBSTRATE_UNAVAILABLE`.
  - Tests: 4 nuevos en `pkg/xolu/describe_test.go` (primero en rojo). `go test -race ./...` en verde. Verificado por MCP contra xolu real: `assets` → 26 campos, 18 obligatorios, `status` ∈ {active, maintenance}; sin `name` no se pide ninguna sugerencia.

- [x] **T9.5. `list` honra `limit`, `offset` y `filter`.** RF-3
  - xolu pagina con `page`/`per_page` (máximo 100; por encima ignora el valor y usa 10) y trata cualquier otro parámetro de consulta como filtro de igualdad, así que `limit=2` devolvía una lista vacía o la página por defecto. `HTTPXoluClient` traduce `limit` (por defecto 50) y `offset` a las páginas que cubren la ventana y la recorta exacta; `filter` pasa como filtros de igualdad; las claves `page` y `per_page` en `filter` devuelven `CONTRACT_VIOLATION`. La respuesta conserva `data` y `pagination` (`total_items`, `offset`, `limit`).
  - Verificado contra xolu real (16 activos): `limit 2`, `limit 2 offset 2`, `offset` no alineado, `filter`, ventana más allá del final (`EMPTY`) y entidad vacía (`EMPTY`). Un agente (Claude Code en modo no interactivo) lo usó por MCP y obtuvo los 3 activos y el total.

- [x] **T9.6. Los fallos de `tools/call` se devuelven como resultado `isError`.** RF-3
  - Probado con un agente real (Claude Code `-p`): con el error en `error.data` del error JSON-RPC, el agente solo veía el mensaje y no el código tipado. Decisión (confirmada el 25-oct-2026): una vez validados los `params`, todo fallo de `tools/call` (`NOT_OFFERED`, `CONTRACT_VIOLATION` de argumentos, `SUBSTRATE_UNAVAILABLE`, `NOT_FOUND`, `EMPTY`, `INVALID_TRANSITION` y errores sin tipo) se devuelve como resultado `{"isError":true,"content":[{"type":"text","text":...}]}`. Un error tipado viaja completo como JSON en `text` (`code`, `message`, `details`); uno sin tipo lleva su mensaje.
  - Siguen siendo errores JSON-RPC los fallos de protocolo: `params` de `tools/call` mal formados (-32602), método desconocido (-32601), JSON inválido (-32700) y los errores de `tools/list` con Hub (spec 005), que no son llamadas a herramienta.
  - Hecho cuando: `go test -v -race ./pkg/mcp/...` pasa con `server_iserror_test.go`, y un agente lee `XOLU-MOLU-FRONT-NOT_FOUND`, `-EMPTY` y `-CONTRACT_VIOLATION` (verificado).
