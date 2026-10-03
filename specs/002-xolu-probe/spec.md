# Spec 002 — xolu Probe (Sonda de Salud xolu)

Estado: aprobada

## Contexto y objetivo
Implementar la sonda de salud en background (`GET /ready` contra xolu) que gatea todas las tool calls y polls programados. La sonda evita carga a xolu degradado y da señal rápida al agente via `SUBSTRATE_UNAVAILABLE` con `RetryMetadata`. Cobertura obligatoria: arranque con backoff, cadencia normal, backoff exponencial en fallo, recuperación, y configuración vía `MOLU_FRONT_*`.

## Usuarios / Actores
- molu (frontal MCP) — ejecuta probe, gatea llamadas, emite taxonomía.
- Agente MCP — recibe `SUBSTRATE_UNAVAILABLE` cuando probe no healthy.
- xolu (sustrato) — expone `/ready` (200 healthy, 503 inicializando/storage down).

## Historias de usuario
- HU-1: Como operador, quiero que molu espere a xolu al arranque con backoff configurable y salga si no levanta tras `MAX_ATTEMPTS`.
- HU-2: Como agente, quiero `SUBSTRATE_UNAVAILABLE` inmediato con `lastSuccessfulContact`/`nextRetryAt` cuando xolu cae, sin timeout por llamada.
- HU-3: Como operador, quiero ajustar cadencia/backoff por env vars sin recompilar.

## Ejes de Control Involucrados
- **Eje 2 (Qué puede ejecutarse)**: Probe gatea invocaciones a primitivos genéricos (`walk`, `find`, `get`) y funciones de dominio.

## Requisitos funcionales (EARS)
- RF-1: CUANDO molu inicia, EL SISTEMA ejecuta probe inicial contra xolu con backoff hasta `MOLU_FRONT_STARTUP_MAX_ATTEMPTS`; si no hay éxito, sale con código ≠0.
- RF-2: CUANDO probe está healthy, EL SISTEMA permite tool calls y polls a xolu.
- RF-3: CUANDO probe falla (timeout, 5xx, error transporte), EL SISTEMA entra en backoff exponencial (`MOLU_FRONT_PING_FAIL_FLOOR` → `MOLU_FRONT_PING_FAIL_CEILING`) y marca `Healthy=false`.
- RF-4: CUANDO probe recupera (200 OK), EL SISTEMA resetea cadencia a `MOLU_FRONT_PING_INTERVAL` y `Healthy=true`.
- RF-5: CUANDO tool call o poll llega con probe `Healthy=false`, EL SISTEMA retorna `SUBSTRATE_UNAVAILABLE` con prefijo `XOLU-MOLU-FRONT-SUBSTRATE_UNAVAILABLE` y `RetryMetadata` completo (`lastSuccessfulContact`, `nextRetryAt`, `attemptNumber`, `maxAttempts`).
- RF-6: EL SISTEMA expone configuración vía `MOLU_FRONT_PING_INTERVAL`, `MOLU_FRONT_PING_TIMEOUT`, `MOLU_FRONT_PING_FAIL_FLOOR`, `MOLU_FRONT_PING_FAIL_CEILING`, `MOLU_FRONT_STARTUP_MAX_ATTEMPTS` con defaults documentados.

## Requisitos no funcionales
- Concurrencia segura: `go test -v -race ./...` cero data races.
- Neutralidad de dominio: cero menciones a "Seam" en código/constantes/defaults.
- Interfaces desacopladas: `Pinger` (mockeable) en `pkg/exec/probe.go` ya existe; reutilizar/extender.

## Criterios de finalización
- Tests unitarios probe (backoff, cadencia, recovery) en verde con `-race`.
- Test de integración dedicado `SUBSTRATE_UNAVAILABLE` con `RetryMetadata` (RF-5).
- Diagramas secuencia Mermaid (`.mmd` + `.pdf`): arranque, fallo, backoff, recuperación, gating tool call.
- Variables `MOLU_FRONT_*` documentadas con defaults y justificación operativa.