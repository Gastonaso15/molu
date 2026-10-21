# Tareas: Spec 004 — RPI Admit/Score (T-004.2 a T-004.5)

Solo las tarjetas T-004.2, T-004.3, T-004.4 y T-004.5. Las demás tarjetas de la spec 004 agregan sus tareas aquí cuando las planifique su responsable.

- [x] **T1. [T-004.2] Tipos del contrato e interfaz `RPIProvider` con las firmas de la spec.** Definiciones, RF-10
  - Hecho cuando: `go test -v -race ./pkg/rpi/... -run "TestToolCandidateJSONFields|TestAdmitContextJSONFields|TestScoredToolEmbedsCandidate"` pasa y `go build ./...` compila.

- [x] **T2. [T-004.2] `MockRPIProvider` en `pkg/rpi/provider_mock.go`.** Definiciones (mockeable), RF-9
  - Hecho cuando: `go test -v -race ./pkg/rpi/... -run "TestMockRPIProvider"` pasa, cubriendo:
    - Delegación en `AdmitFunc` y `ScoreFunc`.
    - Registro de las llamadas.
    - Error cuando la función no está definida.
    - `Reset()`.
    - Uso concurrente.

- [x] **T3. [T-004.3] `admit-all`: `Admit` devuelve todo y `Score` asigna 1.0.** RF-5, RF-7
  - Hecho cuando: `go test -v -race ./pkg/rpi/providers/... -run "TestAdmitAll"` pasa, cubriendo:
    - Catálogo completo en el mismo orden.
    - Catálogo vacío o `nil`, que devuelve un slice vacío no `nil`.
    - Aislamiento de la entrada.
    - `Score` 1.0 conservando el orden.
    - Aserción de interfaz en compilación.

- [x] **T4. [T-004.3] Concurrencia y neutralidad de `admit-all`.** RF-9, RF-10
  - Hecho cuando: `go test -v -race ./pkg/rpi/providers/... -run "TestAdmitAllConcurrent"` pasa sin data races y `grep -ri seam pkg/rpi` no devuelve nada.

- [x] **T5. [T-004.4] Formato de reglas: `ParseStaticContextRules`.** RF-6
  - Hecho cuando: `go test -v -race ./pkg/rpi/providers/... -run "TestParseStaticContextRules"` pasa, cubriendo JSON válido, `null`, JSON mal formado, clave sin `:`, tenant o rol vacíos y nombre de herramienta vacío.

- [x] **T6. [T-004.4] `static-context-rules`: `Admit` filtra por tenant y rol, con tests de rechazo.** RF-6
  - Hecho cuando: `go test -v -race ./pkg/rpi/providers/... -run "TestStaticRulesAdmit"` pasa, cubriendo:
    - Admite solo los nombres permitidos, en el orden del catálogo (primitivos y `namespace.Nombre`).
    - Unión de varios roles.
    - Rechazo: herramienta no listada, otro tenant, otro rol, sin roles, tenant vacío, sin reglas, mayúsculas distintas.
    - Slice vacío no `nil` y aislamiento de las reglas del llamador.

- [x] **T7. [T-004.4] `static-context-rules`: `Score`, interfaz y concurrencia.** RF-5, RF-6, RF-9, RF-10
  - Hecho cuando: `go test -v -race ./pkg/rpi/providers/... -run "TestStaticRules"` pasa, cubriendo `Score` 1.0/0.0 con orden estable, aserción `rpi.RPIProvider`, `SetLogger`/`SetMetrics` y uso concurrente; y `grep -ri seam pkg/rpi` no devuelve nada.

- [x] **T8. [T-004.5] `rpi.Offer` traduce el rechazo del RPI a `NOT_OFFERED`.** RF-4
  - Hecho cuando: `go test -v -race ./pkg/rpi/... -run "TestOffer"` pasa, cubriendo herramienta admitida (sin error), excluida (`XOLU-MOLU-FRONT-NOT_OFFERED` con `tenant` y `tool` en `details`) y error del proveedor devuelto sin cambios.

- [x] **T9. [T-004.5] Test de integración NOT_OFFERED.** RF-4, Criterio 2
  - Hecho cuando: `go test -v -race ./pkg/rpi_integration/... -run "TestIntegration_NotOffered"` pasa: la herramienta existe en `hub.Registry`, `static-context-rules` la rechaza y el código es exactamente `NOT_OFFERED` (no `NOT_FOUND`, no `EMPTY`), también serializado a JSON; con un rol permitido o con admit-all se ofrece.
