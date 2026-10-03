# Tareas: Spec 002 — xolu Probe

- [x] **T1. Test unitario: Check() retorna SUBSTRATE_UNAVAILABLE con RetryMetadata (rojo).** RF-5
  - Hecho cuando: `go test -v -race ./pkg/exec/... -run "TestCheckReturnsSubstrateUnavailable"` falla porque `Check()` no retorna `*taxonomy.TypedError` con código correcto y 4 campos en `details`.

- [x] **T2. Implementar Check() con taxonomy.NewSubstrateUnavailable (verde).** RF-5
  - Hecho cuando: `go test -v -race ./pkg/exec/... -run "TestCheckReturnsSubstrateUnavailable"` pasa en verde.

- [x] **T3. Test unitario: Check() retorna nil cuando Healthy (rojo → verde).** RF-2
  - Hecho cuando: `go test -v -race ./pkg/exec/... -run "TestCheckReturnsNilWhenHealthy"` pasa.

- [x] **T4. Test unitario: Recuperación resetea backoff y cadencia (rojo → verde).** RF-4
  - Hecho cuando: `go test -v -race ./pkg/exec/... -run "TestProbeRecoveryResetsBackoff"` pasa.

- [x] **T5. Test unitario: Concurrencia probe (race detector).** RF-1..RF-5
  - Hecho cuando: `go test -v -race ./pkg/exec/... -run "TestProbeConcurrency"` pasa sin data races.

- [x] **T6. Añadir AttemptNumber/MaxAttempts a ProbeState y actualizar en pingOnce/Run.** RF-5
  - Hecho cuando: `go build ./pkg/exec/...` compila y tests T1-T5 pasan.

- [x] **T7. Test de integración: SUBSTRATE_UNAVAILABLE via probe down (rojo → verde).** RF-5, Criterio 2
  - Hecho cuando: `go test -v -race ./pkg/exec/... -run "TestIntegration_SubstrateUnavailable_WithProbe"` pasa (ya verde porque wiring en main.go existe).

- [x] **T8. Wiring en cmd/molu/main.go: WaitReady al arranque, Run en goroutine, Check en ejecutores.** RF-1, RF-2
  - Hecho cuando: `go build ./cmd/molu` compila; `WaitReady` llamado antes de servir; `Run` en background; ejecutores de tool calls y polls usan `probe.Check()`. **Ya implementado en main.go actual**.

- [x] **T9. Test de integración pasa (verde).** RF-5, Criterio 2
  - Hecho cuando: `go test -v -race ./pkg/exec/... -run "TestIntegration_SubstrateUnavailable_WithProbe"` pasa en verde.

- [x] **T10. Diagrama de secuencia Mermaid (.mmd) y render a PDF.** RF-1..RF-5
  - Hecho cuando: `docs/diagrams/002-xolu-probe-sequence.mmd` existe (PDF generado en CI con `mmdc`).

- [x] **T11. Ejecutar suite completa con race detector y cobertura.** RF-1..RF-6
  - Hecho cuando: `go test -v -race ./...` pasa limpio (cero data races) y cobertura > 70% en pkg/exec/, > 60% en pkg/taxonomy/.