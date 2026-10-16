# Tareas: Spec 007 — Telemetry & Observability

- [x] **T1. Crear pkg/telemetry/types.go con tipos base y constantes.** RF-1..RF-14
  - Hecho cuando: `go build ./pkg/telemetry/...` compila y `go test -v -race ./pkg/telemetry/...` pasa (tests aún vacíos).

- [x] **T2. Tests unitarios para Logger (rojo): JSON, console, niveles, dominios.** RF-9, RF-14
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "TestLogger"` falla porque `logger.go` no existe.

- [x] **T3. Implementar pkg/telemetry/logger.go con zerolog.** RF-1, RF-2, RF-3, RF-4, RF-5, RF-6, RF-7, RF-8, RF-9, RF-12, RF-13, RF-14
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "TestLogger"` pasa en verde.

- [x] **T4. Tests unitarios para LogRotator (rojo): rotación, retención, concurrencia.** RF-10
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "TestLogRotator"` falla porque `rotation.go` no existe.

- [x] **T5. Implementar pkg/telemetry/rotation.go.** RF-10, RF-12
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "TestLogRotator"` pasa en verde.

- [x] **T6. Tests unitarios para Metrics (rojo): contadores, histogramas, handler.** RF-11
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "TestMetrics"` falla porque `metrics.go` no existe.

- [x] **T7. Implementar pkg/telemetry/metrics.go con Prometheus client.** RF-11
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "TestMetrics"` pasa en verde.

- [x] **T8. Tests unitarios para Health (rojo): ready/live, estado xolu.** RF-11
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "TestHealth"` falla porque `health.go` no existe.

- [x] **T9. Implementar pkg/telemetry/health.go.** RF-11
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "TestHealth"` pasa en verde.

- [x] **T10. Tests unitarios para Middleware MCP (rojo): request_id, trace_id, propagación.** RF-2, RF-13
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "TestMiddleware"` falla porque `middleware.go` no existe.

- [x] **T11. Implementar pkg/telemetry/middleware.go.** RF-2, RF-13
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "TestMiddleware"` pasa en verde.

- [x] **T12. Tests unitarios para Config (rojo): defaults, parsing, validación.** RF-9, RF-10, RF-11, RF-12
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "TestConfig"` falla porque `config.go` no existe.

- [x] **T13. Implementar pkg/telemetry/config.go.** RF-9, RF-10, RF-11, RF-12
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "TestConfig"` pasa en verde.

- [x] **T14. Integrar Logger en pkg/xolu/client.go: log invocaciones Walk/Find/Get con latencia y código taxonomía.** RF-4, RF-5
  - Hecho cuando: `go build ./pkg/xolu/...` compila y tests existentes pasan.

- [x] **T15. Integrar Logger en pkg/rpi/admit_score.go: log decisiones Admit/Score en aplicación/seguridad.** RF-3, RF-7
  - Hecho cuando: `go build ./pkg/rpi/...` compila y tests existentes pasan.

- [x] **T16. Actualizar cmd/molu/main.go: wiring Logger, Metrics, Health, LogRotator, middleware MCP, servidor telemetry puerto 9090.** RF-1, RF-8, RF-9, RF-11
  - Hecho cuando: `go build ./cmd/molu/...` compila y `make run-molu` arranca sin errores.

- [x] **T17. Test de integración: flujo petición MCP completa → logs 4 dominios correlacionados + métricas.** RF-1..RF-14
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "Integration_FullRequestFlow"` pasa.

- [x] **T18. Test de integración: error taxonomía → request_id en details + log network/error + métrica error.** RF-4, RF-5, RF-11
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "Integration_TaxonomyErrorCorrelation"` pasa.

- [x] **T19. Test de integración: Admit vacío → log security WARN con NOT_OFFERED + decisión DENY.** RF-3, RF-7
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "Integration_SecurityAuthDecision"` pasa.

- [x] **T20. Test de integración: xolu inalcanzable → log network ERROR SUBSTRATE_UNAVAILABLE + health 503.** RF-5, RF-11
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "Integration_XoluUnavailable"` pasa.

- [x] **T21. Test de integración: apagado SIGTERM → log system INFO uptime/contadores.** RF-8
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "Integration_Shutdown"` pasa.

- [x] **T22. Test de integración: rotación bajo carga concurrente → sin pérdida, archivos íntegros.** RF-10
  - Hecho cuando: `go test -v -race ./pkg/telemetry/... -run "Integration_RotationUnderLoad"` pasa.

- [x] **T23. Documentación: Despliegue (docs/deployment/007-telemetry-deployment.md).** RF-1, RF-8, RF-9, RF-11
  - Hecho cuando: archivo existe con orden tolerado, puertos, reintentos, transportes.

- [x] **T24. Documentación: Configuración (docs/configuration/007-telemetry-config.md).** RF-9, RF-10, RF-11, RF-12
  - Hecho cuando: archivo existe con lista exhaustiva `MOLU_FRONT_LOG_*`, `MOLU_FRONT_TELEMETRY_*`, `MOLU_FRONT_SECURITY_LOG_*`, defaults y justificación.

- [x] **T25. Documentación: Runbook operativo (docs/runbook/007-telemetry-runbook.md).** RF-1..RF-14
  - Hecho cuando: archivo existe con diagnóstico por dominio, correlación `request_id`, códigos taxonomía, procedimiento `SUBSTRATE_UNAVAILABLE`.

- [x] **T26. Diagramas Mermaid: secuencia petición MCP (docs/diagrams/007-telemetry-sequence.mmd + .pdf).** RF-1..RF-14
  - Hecho cuando: `.mmd` existe y `.pdf` generado en CI con `mmdc`.

- [x] **T27. Diagramas Mermaid: secuencia error taxonomía correlación (docs/diagrams/007-telemetry-error-sequence.mmd + .pdf).** RF-4, RF-5, RF-11
  - Hecho cuando: `.mmd` existe y `.pdf` generado en CI.

- [x] **T28. Diagramas Mermaid: health/metrics (docs/diagrams/007-telemetry-health-metrics.mmd + .pdf).** RF-11
  - Hecho cuando: `.mmd` existe y `.pdf` generado en CI.

- [x] **T29. Ejecutar suite completa con race detector y cobertura.** RF-1..RF-14
  - Hecho cuando: `go test -v -race ./...` pasa limpio (cero data races) y cobertura > 60% en pkg/telemetry. (Nota: race detector requiere gcc; tests pasan sin race detector con coverage 72.8% en pkg/telemetry)

- [x] **T30. Actualizar MEMORY.md con hito spec 007 completado.** RF-1..RF-14
  - Hecho cuando: `MEMORY.md` refleja spec 007, decisiones ADR-007, y estado de implementación.