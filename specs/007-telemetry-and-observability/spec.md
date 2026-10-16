# Spec 007 — Telemetry & Observability (Telemetría y Observabilidad)

Estado: borrador

## Contexto y objetivo
Dotar a **molu** (frontal MCP) y al **Hub** de un sistema de logging estructurado, correlacionado y configurable que cubra cuatro dominios operacionales: **sistema**, **aplicación**, **seguridad** y **red/acceso**. El logging debe ser compatible con entornos de producción (JSON, rotación, niveles) y desarrollo (formato humano, salida a consola), y debe integrarse con la taxonomía tipada de errores (Spec 001) para emitir códigos `XOLU-MOLU-FRONT-*` trazables.

## Usuarios / Actores
- **Operador / SRE** — consulta logs estructurados en agregador (Loki, Elastic, Datadog) para diagnóstico y alertas.
- **Desarrollador** — ve logs legibles en consola durante `make run-molu` con correlación de peticiones.
- **Agente de IA (cliente MCP)** — recibe errores tipados con `request_id` para reintentos inteligentes.
- **Auditor de seguridad** — revisa logs de acceso, autenticación y autorización con retención configurable.

## Historias de usuario
- HU-1: Como operador, quiero logs en JSON con `timestamp` RFC3339, `level`, `component`, `request_id`, `trace_id` y `span_id` para correlacionar en el agregador.
- HU-2: Como desarrollador, quiero logs en consola con colores y formato compacto al ejecutar `make run-molu`.
- HU-3: Como SRE, quiero rotación por tamaño/tiempo y retención configurable sin reiniciar el servicio.
- HU-4: Como auditor, quiero logs de seguridad inmutables (append-only) con campos: `actor`, `action`, `resource`, `decision`, `reason`.
- HU-5: Como agente, quiero que cada error tipado (`NOT_FOUND`, `SUBSTRATE_UNAVAILABLE`, etc.) lleve `request_id` en `details` para rastreo distribuido.
- HU-6: Como operador, quiero métricas básicas (contadores de peticiones, latencias, errores por código) expuestas en `/metrics` (Prometheus) y health-check en `/healthz`.

## Ejes de Control Involucrados
- **Eje 1 (Qué se ofrece)**: El logger se inyecta en proveedores RPI (`Admit`/`Score`) para auditar decisiones de admisión/scoring.
- **Eje 2 (Qué puede ejecutarse)**: El cliente xolu (`XoluClient`) loggea invocaciones `walk`/`find`/`get` con latencia y código de resultado (taxonomía).

## Convención de Nombres de Herramienta
- Primitivos genéricos (sin namespace): `walk`, `find`, `get` — loggeados en dominio **red/acceso** y **aplicación**.
- Funciones de dominio (con namespace, ej. `work_orders.CreateOrder`) — loggeados en **aplicación** y **seguridad** (autorización).

## Definiciones
- **Componente**: `molu-front`, `hub-registry`, `xolu-client`, `rpi-provider`, `mcp-transport`.
- **Niveles**: `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL` (mapeo estándar).
- **Campos base obligatorios** (todo log):
  ```json
  {
    "timestamp": "2026-10-15T14:30:00.123Z",
    "level": "INFO",
    "component": "molu-front",
    "request_id": "req-abc123",
    "trace_id": "trace-xyz789",
    "span_id": "span-456",
    "message": "incoming mcp request",
    "fields": { "method": "tools/call", "tool": "walk" }
  }
  ```
- **Dominios de log**:
  - **Sistema**: arranque, apagado, configuración, health, métricas.
  - **Aplicación**: lógica de negocio, llamadas a herramientas, validaciones, taxonomía.
  - **Seguridad**: autenticación, autorización (RPI Admit), acceso a recursos, cambios de config sensibles.
  - **Red/Acceso**: peticiones/respuestas MCP, comunicación con xolu (latencia, reintentos, códigos de error).

## Requisitos funcionales (notación EARS)
- RF-1: CUANDO molu o Hub inician, EL SISTEMA emite log de **sistema** nivel INFO con versión, build info, flags `MOLU_FRONT_*` efectivos y PID.
- RF-2: CUANDO llega una petición MCP, EL SISTEMA genera `request_id` (UUID v7) y `trace_id` (W3C traceparent si viene en headers) y loggea en **red/acceso** nivel INFO con método, tool, tenant.
- RF-3: CUANDO se resuelve una herramienta via RPI `Admit`/`Score`, EL SISTEMA loggea en **aplicación** nivel DEBUG la lista admitida y scores; si `Admit` retorna vacío, loggea en **seguridad** nivel WARN con `NOT_OFFERED`.
- RF-4: CUANDO se invoca primitivo genérico (`walk`, `find`, `get`) o función de dominio, EL SISTEMA loggea en **aplicación** nivel INFO inicio/fin con latencia ms, código taxonomía resultado y `request_id`.
- RF-5: CUANDO xolu es inalcanzable o devuelve error, EL SISTEMA loggea en **red/acceso** nivel ERROR con `SUBSTRATE_UNAVAILABLE` o código verbatim, `attemptNumber`, `nextRetryAt`, `lastSuccessfulContact`.
- RF-6: CUANDO validación de schema falla, EL SISTEMA loggea en **aplicación** nivel WARN con `CONTRACT_VIOLATION` y lista de campos infractores en `fields`.
- RF-7: CUANDO hay decisión de autorización (RPI Admit, validación tenant/rol), EL SISTEMA loggea en **seguridad** nivel INFO con `actor`, `action`, `resource`, `decision` (`ALLOW`/`DENY`), `reason`.
- RF-8: CUANDO molu/Hub reciben señal de apagado (SIGTERM/SIGINT), EL SISTEMA emite log de **sistema** nivel INFO con uptime, contadores de peticiones procesadas y razón de apagado.
- RF-9: EL SISTEMA soporta dos formatos de salida configurables vía `MOLU_FRONT_LOG_FORMAT` (`json` | `console`) y nivel vía `MOLU_FRONT_LOG_LEVEL` (`debug` | `info` | `warn` | `error`).
- RF-10: EL SISTEMA rota logs por tamaño (`MOLU_FRONT_LOG_MAX_SIZE_MB`, default 100) y tiempo (`MOLU_FRONT_LOG_MAX_AGE_DAYS`, default 7), compresión gzip opcional (`MOLU_FRONT_LOG_COMPRESS`).
- RF-11: EL SISTEMA expone endpoint HTTP `/healthz` (liveness/readiness) y `/metrics` (Prometheus) en puerto `MOLU_FRONT_TELEMETRY_PORT` (default 9090), desacoplado del puerto MCP.
- RF-12: LOS LOGS de **seguridad** se escriben en archivo separado (`security.log`) con retención extendida (`MOLU_FRONT_SECURITY_LOG_MAX_AGE_DAYS`, default 90) y sin buffering (flush inmediato).
- RF-13: EL SISTEMA propaga `request_id` y `trace_id` en headers salientes a xolu (`X-Request-ID`, `X-Trace-ID`) para trazabilidad distribuida.
- RF-14: EL SISTEMA usa structured logging con `zerolog` (o `zap` si se justifica) — cero dependencias pesadas, API tipo `log.Info().Str("k", "v").Msg("msg")`.

## Requisitos no funcionales
- **Concurrencia segura**: logger thread-safe, cero data races bajo `go test -v -race ./...`.
- **Neutralidad de dominio**: ningún log hardcodea "Seam"; componentes genéricos (`molu-front`, `hub-registry`).
- **Performance**: logging asíncrono (buffered) para dominios sistema/aplicación/red; síncrono (directo) para seguridad.
- **Configuración**: todas las variables `MOLU_FRONT_LOG_*` y `MOLU_FRONT_TELEMETRY_*` documentadas con defaults y justificación operativa (entregable Configuración §2.3.1).
- **Go 1.27+**, SDK MCP oficial, `zerolog` (o `zap`) como única dependencia de logging.

## Casos límite
- Petición MCP sin header `traceparent` → generar `trace_id` nuevo, propagar en respuesta.
- Rotación de logs durante escritura concurrente → sin pérdida ni corrupción.
- Apagado abrupto (SIGKILL) → logs de seguridad ya flusheados (sync write); resto pueden perder últimas líneas (aceptable).
- xolu devuelve código `XOLU-*` no mapeado → log verbatim en **red/acceso** con `preserved=true` en `fields`.

## Fuera de alcance
- Tracing distribuido completo (Jaeger/Zipkin) — solo propagación de `trace_id`/`span_id` (RF-13).
- Alerting/reglas de notificación — responsabilidad de la plataforma de observabilidad externa.
- Logs de auditoría de negocio a nivel de entidad (pertenece al publicador de dominio).
- Implementación de primitivos genéricos o RPI — specs separadas.

## Criterios de finalización
- Todos los RF cubiertos con tests unitarios en verde (`go test -v -race ./...`).
- Tests de integración: petición MCP → log correlacionado en 4 dominios; apagado → log sistema; error taxonomía → `request_id` en `details`.
- Paquete `pkg/telemetry/` con `Logger`, `Metrics`, `Health` interfaces y implementación `zerolog`.
- Documentación: Despliegue (orden tolerado, puertos), Configuración (lista exhaustiva `MOLU_FRONT_LOG_*`, `MOLU_FRONT_TELEMETRY_*`), Runbook (diagnóstico por dominio, correlación `request_id`).
- Diagramas de secuencia Mermaid (`.mmd` + `.pdf`): flujo petición MCP con logs, flujo error taxonomía con correlación, flujo health/metrics.

## Dudas resueltas (ADR-007)
- **Librería**: `zerolog` (más ligero, API fluida, mejor performance que `zap` para JSON).
- **Formato timestamp**: RFC3339Nano (`2026-10-15T14:30:00.123456789Z`) en JSON; humano legible en console.
- **request_id**: UUID v7 (timestamp ordenable) generado en middleware MCP entrante.
- **Security log**: archivo separado, `os.O_APPEND|os.O_CREATE|os.O_WRONLY` con `sync` en cada write.
- **Metrics**: Prometheus client Go nativo (`prometheus/client_golang`), contadores/histogramas básicos.
- **Health**: `/healthz` devuelve 200 si `XoluClient.Ping()` OK en últimos 30s; 503 si `SUBSTRATE_UNAVAILABLE` activo.