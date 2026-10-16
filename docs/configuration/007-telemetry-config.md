# Configuración de Telemetría y Observabilidad (Spec 007)

## Visión General

Este documento lista **exhaustivamente** todas las variables de entorno `MOLU_FRONT_*` relacionadas con telemetría, logging y observabilidad, con sus valores por defecto, rangos válidos y justificación operativa.

---

## Variables de Logging (`MOLU_FRONT_LOG_*`)

| Variable | Tipo | Default | Valores válidos | Justificación operativa |
|----------|------|---------|-----------------|------------------------|
| `MOLU_FRONT_LOG_FORMAT` | string | `json` | `json`, `console` | **json**: estructurado para agregadores (Loki, Elastic, Datadog). **console**: legible para desarrollo local con colores. |
| `MOLU_FRONT_LOG_LEVEL` | string | `info` | `debug`, `info`, `warn`, `error` | Nivel mínimo de log. `debug` en desarrollo; `info` en producción. `warn`/`error` solo para reducir volumen. |
| `MOLU_FRONT_LOG_OUTPUT_PATH` | string | `""` (stdout) | Ruta absoluta válida | Directorio para archivos de log. Si vacío, salida a stdout (contenedores). Debe ser writable por el proceso. |
| `MOLU_FRONT_LOG_MAX_SIZE_MB` | int | `100` | `1` – `10000` | Tamaño máximo por archivo antes de rotar. 100 MB balancea I/O vs retención. |
| `MOLU_FRONT_LOG_MAX_AGE_DAYS` | int | `7` | `0` – `365` | Días de retención para logs genéricos. `0` = sin límite por edad (solo por tamaño). 7 días cubre semana operativa. |
| `MOLU_FRONT_LOG_COMPRESS` | bool | `true` | `true`, `false` | Comprime archivos rotados con gzip. Ahorra ~80% espacio. Desactivar solo si I/O CPU es bottleneck. |
| `MOLU_FRONT_LOG_FILENAME` | string | `""` (auto) | Nombre de archivo válido | Base name para logs. Si vacío, usa `ComponentMoluFront` (`molu-front.log`, etc.). |
| `MOLU_FRONT_SECURITY_LOG_MAX_AGE_DAYS` | int | `90` | `7` – `3650` | Retención extendida para logs de seguridad (auditoría, compliance). 90 días = 3 meses; ajustar según política de retención legal. |

---

## Variables de Telemetría (`MOLU_FRONT_TELEMETRY_*`)

| Variable | Tipo | Default | Valores válidos | Justificación operativa |
|----------|------|---------|-----------------|------------------------|
| `MOLU_FRONT_TELEMETRY_PORT` | int | `9090` | `1` – `65535` | Puerto HTTP para `/metrics` y `/healthz`. **Debe ser distinto del puerto MCP** para aislar monitoreo de tráfico de aplicación. |
| `MOLU_FRONT_TELEMETRY_METRICS_PATH` | string | `/metrics` | Path HTTP válido (inicia con `/`) | Endpoint Prometheus. Estándar de facto; cambiar solo si choca con rutas MCP. |
| `MOLU_FRONT_TELEMETRY_HEALTH_PATH` | string | `/healthz` | Path HTTP válido (inicia con `/`) | Endpoint health check (Kubernetes liveness/readiness). Estándar CNCF. |
| `MOLU_FRONT_TELEMETRY_ENABLED` | bool | `true` | `true`, `false` | Activa/desactiva servidor telemetry. `false` para entornos sin monitoreo (ej. tests unitarios). |

---

## Variables de Conexión a xolu (`MOLU_FRONT_*`)

| Variable | Tipo | Default | Valores válidos | Justificación operativa |
|----------|------|---------|-----------------|------------------------|
| `MOLU_FRONT_XOLU_URL` | string | *(requerido)* | URL HTTP válida | Base URL de xolu (ej. `http://xolu:8080`). **Requerida**; sin default para forzar configuración explícita. |
| `MOLU_FRONT_PING_INTERVAL` | duration | `10s` | `1s` – `300s` | Intervalo entre health checks de xolu. 10s balancea detección rápida vs carga. |
| `MOLU_FRONT_PING_TIMEOUT` | duration | `5s` | `1s` – `60s` | Timeout por intento de ping. Debe ser < `PING_INTERVAL`. |
| `MOLU_FRONT_PING_FAIL_FLOOR` | duration | `1s` | `100ms` – `60s` | Backoff mínimo tras fallo. 1s evita storm de reconexión. |
| `MOLU_FRONT_PING_FAIL_CEILING` | duration | `30s` | `1s` – `3600s` | Backoff máximo. 30s evita espera excesiva tras recuperación. |
| `MOLU_FRONT_STARTUP_MAX_ATTEMPTS` | int | `10` | `1` – `100` | Reintentos máximos al arranque antes de fallar. 10 × (1s→30s) ≈ 3 min máx. |
| `MOLU_FRONT_PONG_FRESHNESS` | duration | `30s` | `1s` – `300s` | Ventana de validez del último pong exitoso. Usado por health check para decidir `ready`. |

---

## Variables de Schema (Spec 003)

| Variable | Tipo | Default | Valores válidos | Justificación operativa |
|----------|------|---------|-----------------|------------------------|
| `MOLU_FRONT_SCHEMA_REFRESH_INTERVAL` | duration | `5m` | `10s` – `24h` | Intervalo hot-reload de schemas. 5m balancea frescura vs carga en xolu. |
| `MOLU_FRONT_SCHEMA_TIMEOUT` | duration | `10s` | `1s` – `60s` | Timeout para GET `/schemas`. |
| `MOLU_FRONT_SCHEMA_RETRY_FLOOR` | duration | `1s` | `100ms` – `60s` | Backoff mínimo al fallar carga schemas. |
| `MOLU_FRONT_SCHEMA_RETRY_CEILING` | duration | `30s` | `1s` – `3600s` | Backoff máximo schemas. |
| `MOLU_FRONT_SCHEMA_MAX_ATTEMPTS` | int | `10` | `1` – `100` | Reintentos máximos carga inicial. |

---

## Variables de Hub (Spec 005)

| Variable | Tipo | Default | Valores válidos | Justificación operativa |
|----------|------|---------|-----------------|------------------------|
| `MOLU_HUB_PORT` | int | `8081` | `1` – `65535` | Puerto HTTP para Hub Registry. |
| `MOLU_HUB_RPI_PROVIDER` | string | `admit-all` | `admit-all`, `static-context-rules` | Proveedor RPI V1. `admit-all` para desarrollo; `static-context-rules` para producción con reglas YAML. |
| `MOLU_HUB_STATIC_RULES_PATH` | string | `""` | Ruta a archivo YAML | Ruta a reglas estáticas (tenant/role → tools permitidos). Requerido si provider=`static-context-rules`. |

---

## Resumen de Valores por Ambiente

### Desarrollo Local
```bash
MOLU_FRONT_LOG_FORMAT=console
MOLU_FRONT_LOG_LEVEL=debug
MOLU_FRONT_LOG_OUTPUT_PATH=
MOLU_FRONT_TELEMETRY_ENABLED=false
MOLU_FRONT_PING_INTERVAL=5s
MOLU_FRONT_STARTUP_MAX_ATTEMPTS=3
```

### Staging / CI
```bash
MOLU_FRONT_LOG_FORMAT=json
MOLU_FRONT_LOG_LEVEL=info
MOLU_FRONT_LOG_OUTPUT_PATH=/var/log/molu
MOLU_FRONT_TELEMETRY_ENABLED=true
MOLU_FRONT_TELEMETRY_PORT=9090
```

### Producción
```bash
MOLU_FRONT_LOG_FORMAT=json
MOLU_FRONT_LOG_LEVEL=info
MOLU_FRONT_LOG_OUTPUT_PATH=/var/log/molu
MOLU_FRONT_LOG_MAX_SIZE_MB=100
MOLU_FRONT_LOG_MAX_AGE_DAYS=7
MOLU_FRONT_LOG_COMPRESS=true
MOLU_FRONT_SECURITY_LOG_MAX_AGE_DAYS=90
MOLU_FRONT_TELEMETRY_ENABLED=true
MOLU_FRONT_TELEMETRY_PORT=9090
MOLU_FRONT_XOLU_URL=http://xolu-prod:8080
MOLU_FRONT_PING_INTERVAL=10s
MOLU_FRONT_STARTUP_MAX_ATTEMPTS=10
```

---

## Validación de Configuración

Al arranque, molu valida:

1. **Puerto telemetry**: `1-65535`, distinto de puerto MCP
2. **Paths HTTP**: Inician con `/`
3. **Niveles de log**: `debug|info|warn|error`
4. **Formatos**: `json|console`
5. **Tamaños/edades**: Positivos, `MAX_AGE_DAYS >= 0`
6. **Duraciones**: Parseables como `time.Duration` (ej. `10s`, `5m`, `1h`)

**Error de validación** → Log nivel ERROR + exit code 1.

---

## Referencia Rápida (Cheatsheet)

```bash
# Ver variables actuales
env | grep MOLU_FRONT | sort

# Validar config sin arrancar
molu --validate-config

# Ver defaults efectivos
molu --print-config
```