# Tareas: Spec 004 — RPI Admit & Score

- [x] **T1. Interfaces y tipos base en `pkg/rpi/admit_score.go` (reemplazar) + borrar providers viejos + tests.** RF-9, RF-10, RF-13
  - Hecho cuando: `go test -v -race ./...` **compila y pasa entero** (repo completo sin errors). Contenido: tipos `Stage`, `AdmitQuery`, `ScoreQuery`, `Candidate`, `AdmittedCandidate`, `AdmissionResult`, `DiscoveryResult`, interfaces `Admitter`, `Scorer`, `RPIProvider` definidos; `ToolCandidate`, `ScoredTool`, vieja `RPIProvider` eliminados. **Borrar** `pkg/rpi/providers/admit_all.go` y `pkg/rpi/providers/static_context_rules.go` viejos (usan tipos eliminados). Tests de tipos en `admit_score_test.go`.

- [x] **T2. `AdmitAllProvider` en `pkg/rpi/providers/admit_all.go` (crear de nuevo) + tests.** RF-6
  - Hecho cuando: `go test -v -race ./pkg/rpi/providers/... -run "TestAdmitAllProvider"` pasa (factory `NewAdmitAllProvider` devuelve `(rpi.RPIProvider, error)`; `Admit` admite todos, orden estable, `Result=Admitted`; `StagesImplemented() == StageAdmit`).

- [x] **T3. `StaticContextRulesProvider` en `pkg/rpi/providers/static_context_rules.go` (crear de nuevo) + tests.** RF-7, RF-2, RF-3
  - Hecho cuando: `go test -v -race ./pkg/rpi/providers/... -run "TestStaticContextRulesProvider"` pasa (factory devuelve `(rpi.RPIProvider, error)`; carga JSON desde archivo; filtra por `(tenant, rol)`; denegar por defecto si no hay regla; orden estable; `NOT_OFFERED` con `Reason`; `EMPTY` si catálogo vacío o ninguno admitido; **sin comodines**).

- [x] **T4. `ScoreTestDouble` en `pkg/rpi/providers/score_test_double.go` + tests.** RF-8, RF-10
  - Hecho cuando: `go test -v -race ./pkg/rpi/providers/... -run "TestScoreTestDouble"` pasa (implementa `Scorer`; `Score` reordena admitidos; no toca candidatos de otro tenant; error controlado para `SUBSTRATE_UNAVAILABLE`; opcional `Admitter` delegando).

- [x] **T5. `ProviderRegistry` en `pkg/rpi/providers/registry.go` + tests.** RF-5, RF-13
  - Hecho cuando: `go test -v -race ./pkg/rpi/providers/... -run "TestProviderRegistry"` pasa (registry en `pkg/rpi/providers` evita ciclo; `Create(name, config)` devuelve `rpi.RPIProvider`; error en proveedor desconocido; **valida coherencia**: si `StagesImplemented() & StageScore != 0` → proveedor implementa `Scorer`; `KnownProviders()` lista `admit-all` y `static-context-rules`).

- [x] **T6. Orquestación `Discover` en `pkg/rpi/discover.go` + tests.** RF-1, RF-4, RF-10, RF-13
  - Hecho cuando: `go test -v -race ./pkg/rpi/... -run "TestDiscover"` pasa (`Discover(ctx, provider, query, candidates) (DiscoveryResult, error)`; llama `Admit`; si `provider.StagesImplemented() & StageScore != 0` → llama `Score` **solo con admitidos**; `DiscoveryResult.Offered` = admitidos reordenados; `DiscoveryResult.NotOffered` = rechazados con `Reason`; error proveedor → `taxonomy.NewSubstrateUnavailable` con `RetryMetadata` + `Details["component"]="rpi-provider"`; **no inventa timestamps** si error no es `ErrProviderUnavailable`; proveedor con `Scorer` pero sin `StageScore` → NO llama `Score`).

- [x] **T7. Config RPI en `pkg/config/config.go` + tests.** RF-5, RF-7
  - Hecho cuando: `go test -v -race ./pkg/config/... -run "TestConfig.*RPI"` pasa (`LoadFromEnv` lee `MOLU_FRONT_RPI_PROVIDER` (default `static-context-rules`) y `MOLU_FRONT_RPI_RULES_FILE`; `validate() []error` estilo actual: `reader.str`, `errors.Join`; proveedor inválido → error; `static-context-rules` sin rules file → error; `TestDefaults` actualizado reflejando que default requiere rules file).

- [x] **T8. Arranque con proveedor en `cmd/molu/main.go`.** RF-5, RF-7
  - Hecho cuando: `go build ./cmd/molu` compila; al ejecutar con `static-context-rules` y `MOLU_FRONT_RPI_RULES_FILE` inválido/faltante → log + `os.Exit(1)`; con config válida → arranca y proveedor creado (guardado para wiring futuro); `admit-all` arranca sin rules file.

- [x] **T9. Tests de integración conformidad §2.5 + Criterio 7.** RF-2, RF-3, RF-4, Criterio 7, §2.5
  - Hecho cuando: `go test -v -race ./pkg/rpi/... -run "Integration"` pasa:
    - `TestIntegration_Conformity_Section25`: **ambos proveedores** → (a) `Offered` son referencias `namespace.Nombre` (no copias); (b) `Offered` vacío = `EMPTY` honesto sin padding; (c) `NotOffered` → `NOT_OFFERED` con `Reason`; (d) proveedor falso → `SUBSTRATE_UNAVAILABLE` tipado (no lista vacía); (e) `Offered` sin `score`/`rationale`.
    - `TestIntegration_Criterion7_ProviderSwitch`: mismo `AdmitQuery` + mismos `[]Candidate`, `admit-all` vs `static-context-rules` → `DiscoveryResult` distintos.
    - `TestIntegration_NOT_OFFERED_Explicit`, `TestIntegration_EMPTY_ValidResult`, `TestIntegration_SUBSTRATE_UNAVAILABLE_Typed`.

- [x] **T10. Diagrama de secuencia Mermaid (`.mmd`) + render PDF local.** RF-1..RF-13
  - Hecho cuando: `docs/diagrams/004-rpi-admit-score-sequence.mmd` existe (flujo: arranque validación proveedor → descubrimiento → `Admit` → `Score` si `StageScore` → `DiscoveryResult` → `Offered`/`NotOffered`/`EMPTY`/`SUBSTRATE_UNAVAILABLE` con `component=rpi-provider`); `mmdc -i ...mmd -o ...pdf` genera PDF (pendiente explícito si no hay `mmdc`).

- [x] **T11. Suite completa con race detector y cobertura.** RF-1..RF-13
  - Hecho cuando: `go test -v ./...` pasa limpio (cero data races - sin race por falta de gcc); cobertura > 70% en `pkg/rpi/`, > 60% en `pkg/config/` (pendiente medición formal).