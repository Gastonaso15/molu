# Tareas: Spec 001 — Taxonomy Types

- [x] **T1. Crear pkg/taxonomy/types.go con tipos base y constantes.** RF-1..RF-8
  - Hecho cuando: `go build ./pkg/taxonomy/...` compila y `go test -v -race ./pkg/taxonomy/...` pasa (tests aún vacíos).

- [x] **T2. Tests unitarios para constructores de error (rojo).** RF-1, RF-3, RF-5, RF-6
  - Hecho cuando: `go test -v -race ./pkg/taxonomy/... -run "TestNew"` falla porque `errors.go` no existe.

- [x] **T3. Implementar pkg/taxonomy/errors.go con constructores propios.** RF-1, RF-3, RF-5, RF-6
  - Hecho cuando: `go test -v -race ./pkg/taxonomy/... -run "TestNew"` pasa en verde.

- [x] **T4. Tests unitarios para wrappers de verbatim xolu (rojo).** RF-2, RF-4, RF-7
  - Hecho cuando: `go test -v -race ./pkg/taxonomy/... -run "TestWrap"` falla.

- [x] **T5. Implementar wrappers verbatim en pkg/taxonomy/errors.go.** RF-2, RF-4, RF-7
  - Hecho cuando: `go test -v -race ./pkg/taxonomy/... -run "TestWrap"` pasa en verde.

- [x] **T6. Tests de serialización JSON y prefijos (rojo).** RF-1..RF-8
  - Hecho cuando: `go test -v -race ./pkg/taxonomy/... -run "TestJSON"` falla.

- [x] **T7. Ajustar errors.go para serialización correcta (verde).** RF-1..RF-8
  - Hecho cuando: `go test -v -race ./pkg/taxonomy/... -run "TestJSON"` pasa.

- [x] **T8. Crear interfaz XoluClient y mock en pkg/xolu/.** RF-2, RF-4, RF-6, RF-7
  - Hecho cuando: `go build ./pkg/xolu/...` compila y mock compila.

- [x] **T9. Crear interfaz MCPTransport y mock en pkg/mcp/.** RF-6
  - Hecho cuando: `go build ./pkg/mcp/...` compila.

- [x] **T10. Crear interfaz RPIProvider y proveedores V1 stub en pkg/rpi/.** RF-1
  - Hecho cuando: `go build ./pkg/rpi/...` compila.

- [x] **T11. Test de integración NOT_OFFERED (rojo → verde).** RF-1
  - Hecho cuando: `go test -v -race ./pkg/taxonomy/... -run "TestIntegration_NotOffered"` pasa.

- [x] **T12. Test de integración NOT_FOUND propio (rojo → verde).** RF-2
  - Hecho cuando: `go test -v -race ./pkg/taxonomy/... -run "TestIntegration_NotFound"` pasa.

- [x] **T13. Test de integración NOT_FOUND verbatim xolu (rojo → verde).** RF-2, RF-7
  - Hecho cuando: `go test -v -race ./pkg/taxonomy/... -run "TestIntegration_XoluNotFoundVerbatim"` pasa.

- [x] **T14. Test de integración EMPTY (rojo → verde).** RF-3
  - Hecho cuando: `go test -v -race ./pkg/taxonomy/... -run "TestIntegration_Empty"` pasa.

- [x] **T15. Test de integración INVALID_TRANSITION verbatim (rojo → verde).** RF-4, RF-7
  - Hecho cuando: `go test -v -race ./pkg/taxonomy/... -run "TestIntegration_InvalidTransitionVerbatim"` pasa.

- [x] **T16. Test de integración CONTRACT_VIOLATION (rojo → verde).** RF-5
  - Hecho cuando: `go test -v -race ./pkg/taxonomy/... -run "TestIntegration_ContractViolation"` pasa.

- [x] **T17. Test de integración SUBSTRATE_UNAVAILABLE con RetryMetadata (rojo → verde).** RF-6
  - Hecho cuando: `go test -v -race ./pkg/taxonomy/... -run "TestIntegration_SubstrateUnavailable"` pasa.

- [x] **T18. Test de integración verbatim passthrough genérico (rojo → verde).** RF-7
  - Hecho cuando: `go test -v -race ./pkg/taxonomy/... -run "TestIntegration_XoluVerbatimPassthrough"` pasa.

- [x] **T19. Test de no-colapso a error genérico (rojo → verde).** RF-8
  - Hecho cuando: `go test -v -race ./pkg/taxonomy/... -run "TestIntegration_NoGenericError"` pasa.

- [x] **T20. Diagrama de secuencia Mermaid (.mmd) y render a PDF.** RF-1..RF-8
  - Hecho cuando: `docs/diagrams/001-taxonomy-sequence.mmd` existe (PDF generado en CI con `mmdc`).

- [x] **T21. Ejecutar suite completa con race detector y cobertura.** RF-1..RF-8
  - Hecho cuando: `go test -v -race ./...` pasa limpio (cero data races) y cobertura > 60% en pkg/taxonomy.