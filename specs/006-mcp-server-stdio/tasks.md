# Tareas: Spec 006 — Servidor MCP Stdio y Despachador de Primitivos en Molu Frontend

- [x] **T1. Estructura del Servidor MCP y descubrimiento de herramientas (`tools/list`).** RF-1, RF-2
  - Definir interfaces `PrimitiveProvider`, `HealthChecker` y `SubstrateExecutor` en `pkg/mcp/server.go`.
  - Implementar constructor `NewServer(cfg ServerConfig)` y método `HandleListTools(ctx context.Context)`.
  - Hecho cuando: `pkg/mcp/server_test.go` verifique con `go test -v -race ./pkg/mcp/...` que `tools/list` expone los primitivos (`walk`, `find`, `get`) con formato MCP correcto sin namespace, o lista vacía si no hay esquemas.

- [ ] **T2. Despacho de herramientas (`tools/call`): Gating de salud y herramientas no ofrecidas.** RF-4, RF-5
  - Implementar en `server.go` la comprobación previa de salud con `HealthChecker.Check()`; si xolu está caído, retornar `SUBSTRATE_UNAVAILABLE`.
  - Si la herramienta solicitada no existe entre los primitivos conocidos, retornar `NOT_OFFERED`.
  - Hecho cuando: Tests dedicados en `server_test.go` pasen en verde con `go test -v -race ./pkg/mcp/...`.

- [ ] **T3. Validación de contratos en `tools/call` y ejecución de primitivos (`get`, `find`, `walk`).** RF-3
  - Integrar `PrimitiveProvider.ValidateInput(toolName, args)`; si la entrada viola el schema, retornar `CONTRACT_VIOLATION` con los campos infractores.
  - Si los argumentos son válidos y el sustrato está saludable, delegar ejecución a `SubstrateExecutor`:
    - `get`: llamada con `entity` e `id` traduciendo a `GET /api/v1/{entity}/{id}` (y mapeo de 404 a `NOT_FOUND`).
    - `find`: llamada con filtros de entidad, OQL o search traduciendo a `GET /api/v1/{entity}?filters` o `POST /api/v1/oql/query` (y mapeo de lista vacía a `EMPTY`).
    - `walk`: llamada con FSM o grafo traduciendo a `POST /api/v2/fsm/machine/{id}/walk` o rutas de grafo (y mapeo de guarda rechazada a `INVALID_TRANSITION`).
  - Hecho cuando: Tests de validación de schema y camino feliz para los tres primitivos pasen en verde con `go test -v -race ./pkg/mcp/...`.

- [ ] **T4. Refresco dinámico de herramientas y verificación de concurrencia.** RF-6, RF-7, RF-8
  - Implementar `UpdatePrimitives(newPrimitives []schema.PrimitiveSchema)` con `sync.RWMutex` para reemplazo atómico.
  - Implementar test de concurrencia intensiva `TestServer_Concurrency` ejecutando `ListTools`, `CallTool` y `UpdatePrimitives` en paralelo.
  - Hecho cuando: `go test -v -race ./pkg/mcp/...` pase con cero data races y neutralidad de dominio estricta.

- [ ] **T5. Cableado en `cmd/molu/main.go` sobre transporte stdio.** RF-1, RF-6
  - Reemplazar los `TODO` en `cmd/molu/main.go` instanciando `mcp.NewServer()`.
  - Garantizar que los logs de `slog` salgan estrictamente por `os.Stderr`.
  - Conectar el refresco en el manejador de la señal `SIGHUP`.
  - Iniciar el bucle de transporte stdio.
  - Hecho cuando: `make build` compile limpiamente y `go test -v -race ./...` pase en verde.

- [ ] **T6. Diagrama de secuencia y documentación.** RF-1 a RF-8
  - Crear diagrama Mermaid en `docs/diagrams/006-mcp-server-stdio-sequence.mmd`.
  - Actualizar `MEMORY.md` y `README.md` con los detalles operativos finales.
  - Hecho cuando: El archivo `.mmd` esté creado y validado en el repositorio.
