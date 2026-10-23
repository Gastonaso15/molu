# Tareas: Spec 005 — Catálogo y Hub (T-005.4)

Solo la tarjeta T-005.4 (Fase 3, «Ensamblar Hub con RPI y Transporte MCP»). Las tarjetas T-005.2 y T-005.3 (`Registry` y `PublisherAPI`) ya están implementadas en `pkg/hub/`. Las demás tarjetas de la spec 005 agregan sus tareas aquí cuando las planifique su responsable.

**Depende de**: T-004.2 (`rpi.RPIProvider`, hecho), T-005.2 / T-005.3 (hecho).
**Desbloquea**: T-007.2 (middleware MCP), T-008.2 y T-008.3 (E2E).

## Decisiones (confirmadas el 23-oct-2026)
- El `AdmitContext` (`tenant`, `roles`, `query`) llega en los `params` de `tools/list`, como define la spec 005 (Definiciones y RF-9). Tenant y roles no se toman de la configuración del proceso.
- `tools/list` ofrece el catálogo del Hub filtrado por RPI. Los primitivos de xolu que ya expone `HandleListTools` no cambian en esta tarjeta.
- La lógica de descubrimiento vive en `pkg/hub` (sin dependencia del transporte); `pkg/mcp` solo la invoca a través de una interfaz.

- [x] **T1. [T-005.4] `hub.Discovery`: catálogo → `Admit` → `Score`.** RF-4, RF-5, RF-6, RF-9, RF-10
  - Hecho cuando: `go test -v -race ./pkg/hub/... -run "TestDiscovery"` pasa, cubriendo:
    - Tenant vacío → `CONTRACT_VIOLATION` con `details.fields: ["tenant"]` y sin invocar al proveedor.
    - Con `admit-all` devuelve todo el catálogo (primitivos y `namespace.Nombre`), ordenado por score descendente.
    - Con `static-context-rules` omite lo no admitido (no devuelve error ni rastro de la herramienta excluida).
    - Lista admitida vacía → `EMPTY`; catálogo vacío → `EMPTY`.
    - Error del proveedor devuelto sin cambios.
    - Uso concurrente con registros simultáneos sin data races.

- [x] **T2. [T-005.4] `tools/list` de MCP consulta `hub.Discovery`.** RF-4, RF-9
  - Hecho cuando: `go test -v -race ./pkg/mcp/... -run "TestToolsListHub"` pasa, cubriendo:
    - Parseo de `params` a `AdmitContext` (`tenant`, `roles`, `query`).
    - Respuesta con `ToolDescriptor` en orden de relevancia.
    - Errores tipados serializados en el error JSON-RPC sin colapsarse a genéricos.
    - Sin Hub configurado, el comportamiento actual de `tools/list` no cambia.

- [x] **T3. [T-005.4] Wiring en `cmd/molu` y test de integración (Criterio 7).** RF-1, RF-4
  - Hecho cuando: `go test -v -race ./pkg/... -run "TestIntegration_ToolsList"` pasa y demuestra que cambiar el proveedor por configuración (`admit-all` ↔ `static-context-rules`) altera el resultado de `tools/list` sin tocar código; `go build ./...` compila y `grep -ri seam pkg cmd` no devuelve nada.
  - Alcance real: variables `MOLU_HUB_RPI_PROVIDER`, `MOLU_HUB_RPI_RULES_FILE` y `MOLU_HUB_RPI_RULES_JSON` en `pkg/config`; fábrica `hub.NewRPIProvider`; wiring en `cmd/molu/main.go`; integración en `pkg/hub_integration`.
  - Sin `MOLU_HUB_RPI_PROVIDER`, el descubrimiento del Hub queda desactivado y `tools/list` sigue sirviendo los primitivos de xolu (la spec 005 propone `admit-all` por defecto; se prefirió no cambiar el comportamiento actual de molu).
