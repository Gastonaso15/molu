# Tareas: Spec 008 — Execution Bridge para Funciones de Dominio del Hub

- [x] **T1. Tipos, interfaces del puente y `EnvSecretResolver`.** RF-5, RF-6, RF-13
  - Definir interfaces en `pkg/bridge/types.go` (`ToolLookup`, `SecretResolver`, `DomainCaller`, `DomainExecutor`) y modelos de autenticación (`AuthMetadata`, `AuthMode`).
  - Implementar `EnvSecretResolver` en `pkg/bridge/resolver.go`: normaliza `token_ref` (ej. `vault:token-1` $\rightarrow$ `MOLU_FRONT_TOKEN_VAULT_TOKEN_1`), con soporte prioritario para archivos con sufijo `_FILE`.
  - Hecho cuando: `go test -v -race ./pkg/bridge/... -run "TestSecretResolver"` pasa en verde y verifica normalización, lectura de env, lectura de `_FILE` y error ante secreto inexistente. (Completado: tests en verde bajo `-race`).

- [ ] **T2. Validador de contratos de dominio (`ValidateDomainInput`).** RF-3, RF-4
  - Implementar `ValidateDomainInput(schemaMap, args)` en `pkg/bridge/validator.go` utilizando `gojsonschema`.
  - Retornar `CONTRACT_VIOLATION` con `details.fields[]` ante violación de tipos o campos obligatorios faltantes.
  - Hecho cuando: `go test -v -race ./pkg/bridge/... -run "TestValidateDomainInput"` pasa en verde con pruebas unitarias por tabla.

- [ ] **T3. Cliente HTTP passthrough (`HTTPDomainCaller`).** RF-7, RF-8, RF-9, RF-10, RF-11
  - Implementar `HTTPDomainCaller` en `pkg/bridge/caller.go`: ejecuta peticiones HTTP POST JSON con timeout y headers de autenticación (`Bearer`, `apikey` o `none`) y correlación (`X-Request-ID`, `traceparent`).
  - Mapear respuestas: status 200 $\rightarrow$ payload JSON; status 404 $\rightarrow$ `NOT_FOUND`; status 5xx / timeout $\rightarrow$ `SUBSTRATE_UNAVAILABLE`; colección vacía $\rightarrow$ `EMPTY`; sobre xolu con código $\rightarrow$ error verbatim.
  - Hecho cuando: `go test -v -race ./pkg/bridge/... -run "TestHTTPDomainCaller"` pasa en verde contra un servidor `httptest.Server` para cada escenario.

- [ ] **T4. Orquestador `ExecutionBridge` con validación RPI y `ToolLookup`.** RF-1, RF-2, RF-12
  - Implementar `ExecutionBridge` en `pkg/bridge/bridge.go` orquestando: consulta a `ToolLookup` $\rightarrow$ gating RPI con `rpi.Offer` $\rightarrow$ validación de esquema $\rightarrow$ resolución de auth $\rightarrow$ llamada HTTP.
  - Hecho cuando: `go test -v -race ./pkg/bridge/... -run "TestBridge"` pasa en verde cubriendo `NOT_OFFERED` (herramienta desconocida o rechazada por RPI), `CONTRACT_VIOLATION`, éxito y prueba de concurrencia bajo `-race`.

- [ ] **T5. Integración en `pkg/mcp/server.go` y despacho de funciones de dominio.** RF-1, RF-2, RF-7
  - Extender `ServerConfig` con `DomainExecutor` y `CatalogLookup`.
  - Actualizar `isToolOffered` para consultar `CatalogLookup` ante herramientas con namespace (`.`) y enrutar su ejecución al `DomainExecutor` en `executeTool`.
  - Hecho cuando: `go test -v -race ./pkg/mcp/... -run "TestServer_Domain"` pasa en verde para llamadas a herramientas de dominio y primitivos nativos sin interferencias.

- [ ] **T6. Configuración y cableado en `cmd/molu/main.go`.** RF-1, RF-7
  - Adición de `MOLU_FRONT_DOMAIN_CALL_TIMEOUT` (default `30s`) en `pkg/config`.
  - Instanciar `bridge.NewExecutionBridge` en `cmd/molu/main.go` conectando el `hub.Registry`, proveedor RPI, logger y métricas, e inyectarlo en `mcp.NewServer`.
  - Hecho cuando: `make build` compila limpiamente y `go test -v -race ./...` pasa en verde.

- [ ] **T7. Pruebas de integración E2E de taxonomía tipada (§2.4 y Criterio 2).** RF-2, RF-4, RF-8, RF-9, RF-10
  - Implementar `pkg/bridge_integration/bridge_integration_test.go` simulando ciclo completo `tools/call` a través de JSON-RPC stdio.
  - Demostrar que los errores tipados (`NOT_OFFERED`, `CONTRACT_VIOLATION`, `NOT_FOUND`, `SUBSTRATE_UNAVAILABLE`, `EMPTY`) se entregan correctamente en el sobre MCP `isError: true` con su JSON estructurado.
  - Hecho cuando: `go test -v -race ./pkg/bridge_integration/...` pasa en verde.

- [ ] **T8. Diagrama de secuencia Mermaid y documentación operativa.** RF-1 a RF-13
  - Crear `docs/diagrams/008-execution-bridge-sequence.mmd` reflejando el flujo completo del Execution Bridge.
  - Actualizar `MEMORY.md` y `README.md` documentando el soporte de funciones de dominio y las variables de configuración.
  - Hecho cuando: El archivo Mermaid existe y la documentación del proyecto está actualizada.
