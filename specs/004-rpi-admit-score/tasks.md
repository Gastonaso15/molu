# Tareas: Spec 004 — RPI Admit/Score (T-004.2 y T-004.3)

Solo las tarjetas T-004.2 y T-004.3. Las demás tarjetas de la spec 004 agregan sus tareas aquí cuando las planifique su responsable.

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
