# Tareas: Spec 003 — Schema Reader

- [x] **T1. Crear tipos de dominio en `pkg/schema/types.go`.**
  - Hecho cuando: `go build ./pkg/schema/...` compila; structs `PrimitiveSchema`, `StateMachine`, `Transition`, `PayloadType`, `Schemas` definidos con tags JSON correctos.

- [x] **T2. Crear interfaz `SchemaClient` y `XoluSchemaClient` en `pkg/schema/client.go`.**
  - Hecho cuando: `go build ./pkg/schema/...` compila; `FetchSchemas(ctx) (Schemas, error)` implementada con timeouts y manejo de errores HTTP.

- [x] **T3. Crear `SchemaLoader` esqueleto en `pkg/schema/loader.go` (métodos vacíos que compilan).**
  - Hecho cuando: `go build ./pkg/schema/...` compila; `NewSchemaLoader`, `Load`, `Refresh`, `GetPrimitives`, `Run`, `Stop` firmados.

- [x] **T4. Test unitario: Load éxito + GetPrimitives (rojo).** RF-1, RF-2
  - Hecho cuando: `go test -v -race ./pkg/schema/... -run "TestLoadSuccess"` falla porque `Load` no hace swap ni `GetPrimitives` no retorna primitivos.

- [x] **T5. Implementar `Load()` con retry/backoff y `swap()` atómico (verde).** RF-1, RF-2, RF-3
  - Hecho cuando: `go test -v -race ./pkg/schema/... -run "TestLoadSuccess"` pasa en verde.

- [x] **T6. Tests unitarios: retry backoff, max attempts, context cancellation (rojo → verde).** RF-3
  - Hecho cuando: `go test -v -race ./pkg/schema/... -run "TestLoadRetryBackoff|TestLoadMaxAttemptsExceeded|TestLoadContextCancellation"` pasan.

- [x] **T7. Test unitario: Refresh atómico + diff add/remove (rojo).** RF-4
  - Hecho cuando: `go test -v -race ./pkg/schema/... -run "TestRefreshAtomicSwap|TestRefreshAddRemovePrimitive"` fallan.

- [x] **T8. Implementar `Refresh()`, `Run()` (ticker), `Stop()`, signal handler SIGHUP en main.go (verde).** RF-4
  - Hecho cuando: tests T7 pasan; `Run()` lanza goroutine con ticker; `Stop()` la detiene limpiamente.

- [x] **T9. Tests unitarios: Validación entrada → CONTRACT_VIOLATION (rojo).** RF-5
  - Hecho cuando: `go test -v -race ./pkg/schema/... -run "TestValidateInputMissingRequired|TestValidateInputTypeMismatch|TestValidateInputEnumViolation|TestValidateInputNestedObject"` fallan porque `ValidateInput` no existe o no retorna `*taxonomy.TypedError` correcto.

- [x] **T10. Implementar `ValidateInput()` con `gojsonschema` en `pkg/schema/validation.go` (verde).** RF-5
  - Hecho cuando: tests T9 pasan; `details.invalidFields[]` poblado correctamente.

- [x] **T11. Test unitario: Validación éxito (rojo → verde).** RF-5
  - Hecho cuando: `go test -v -race ./pkg/schema/... -run "TestValidateInputSuccess"` pasa.

- [x] **T12. Tests unitarios: `XoluSchemaClient` (timeout, 5xx, JSON inválido, estructura inesperada) (rojo → verde).**
  - Hecho cuando: `go test -v -race ./pkg/schema/... -run "TestSchemaClient"` pasan.

- [x] **T13. Test unitario: Concurrencia Loader + race detector.** RF-1..RF-5
  - Hecho cuando: `go test -v -race ./pkg/schema/... -run "TestLoaderConcurrency"` pasa sin data races.

- [x] **T14. Añadir variables `MOLU_FRONT_SCHEMA_*` a `pkg/config/config.go` + tests.** RF-6
  - Hecho cuando: `go build ./pkg/config/...` compila; `go test -v -race ./pkg/config/...` pasa; defaults documentados en código.

- [x] **T15. Wiring en `cmd/molu/main.go`: Load al arranque, Run en goroutine, SIGHUP handler, registro MCP tras carga.** RF-1, RF-4
  - Hecho cuando: `go build ./cmd/molu` compila; `Load()` llamado antes de servir; `Run()` en background; `Refresh()` en SIGHUP; herramientas `walk`, `find`, `get` registradas en MCP server sin namespace.

- [x] **T16. Test de integración: CONTRACT_VIOLATION con invalidFields (rojo → verde).** RF-5, Criterio 2
  - Hecho cuando: `go test -v -race ./pkg/taxonomy/... -run "TestIntegration_ContractViolation_InvalidInput"` pasa (flujo E2E: tool call inválido → error taxonomía con `details.invalidFields[]`).

- [x] **T17. Diagrama de secuencia Mermaid (`.mmd`) y render a PDF.** RF-1..RF-5
  - Hecho cuando: `docs/diagrams/003-schema-reader-sequence.mmd` existe (PDF generado en CI con `mmdc`).

- [x] **T18. Ejecutar suite completa con race detector y cobertura.** RF-1..RF-6
  - Hecho cuando: `go test -v -race ./...` pasa limpio (cero data races) y cobertura > 70% en `pkg/schema/`, > 60% en `pkg/taxonomy/`.

---

## Ajuste al formato real de xolu (T19)

**Decisión (20-nov-2026)**: xolu 0.31.0 y Seam 0.4.5 quedan como están, así que el lector se adapta a lo que xolu ofrece. xolu no publica primitivos, máquinas de estado ni tipos de payload: `GET /api/v1/schemas` solo lista las entidades con esquema registrado (`{"count":N,"schemas":[{"name":...}]}`) y el esquema de cada una sale de `GET /api/v1/schema/{entity}`. Los 13 primitivos genéricos son de molu.

- [x] **T19. `XoluSchemaClient` lee el formato real y el cargador conserva los esquemas por entidad.** RF-1, RF-3, RF-4
  - **Archivos**: `pkg/schema/types.go` (`EntitySchema`, `Schemas.Entities`), `client.go`, `loader.go` (`GetEntitySchemas`), `client_test.go`, `loader_test.go`, `cmd/molu/main.go`
  - **Hecho cuando**: `go test -v -race ./pkg/schema/...` pasa cubriendo el formato real (lista más detalle por entidad, orden determinista, lista vacía sin llamadas de detalle, 404 de una entidad omitida, 5xx que falla la carga y conserva la foto anterior, nombres escapados, 30 entidades con lectura acotada), el formato original que sigue funcionando, y la recarga en caliente de `GetEntitySchemas`. Contra xolu real: 20 esquemas con la semilla 01 `--schemas`, 0 con semillas sin esquemas; `main` lo informa en el log al arrancar y al recargar con SIGHUP.
  - **Límite conocido**: los esquemas se leen y se mantienen, pero ningún primitivo los usa todavía para validar `create`/`update`. Los esquemas registrados por las semillas no traen `required`.

