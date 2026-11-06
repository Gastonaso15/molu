# Configuración de Telemetría y Observabilidad (Spec 007)

## Visión General

Este documento lista **exhaustivamente** todas las variables de entorno `MOLU_FRONT_*` y `MOLU_HUB_*` relacionadas con telemetría, logging, observabilidad, descubrimiento de herramientas y configuración del proveedor RPI, con sus valores por defecto, rangos válidos y justificación operativa.

El presente documento depende de las definiciones de variables en las specs **003** (Schema Reader), **004** (RPI Admit/Score), **005** (Catálogo y Hub), **006** (MCP Server Stdio) y **007** (Telemetry & Observability). Los `SCHEMA_*` variables están definidos en la Spec 003 y son consumidos por el Schema Loader para el hot-reload y retry de `GET /schemas` contra xolu.

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
| `MOLU_FRONT_SCHEMA_REFRESH_INTERVAL` | duration | `5m` | `10s` – `24h` | *También documentado en sección de Schema (Spec 003)*: Intervalo hot-reload de schemas. Compartido entre Spec 003 y Spec 007 para consistencia. |
| `MOLU_FRONT_SCHEMA_TIMEOUT` | duration | `10s` | `1s` – `60s` | *También documentado en sección de Schema (Spec 003)*: Timeout para GET `/schemas`. Compartido entre Spec 003 y Spec 007. |

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

Estas variables controlan la carga y refresh de los esquemas de xolu, utilizados por el **Schema Loader** (Spec 003) para poblar el catálogo de primitivos genéricos. Están directamente relacionadas con los RF-1 a RF-4 de la Spec 003 y dependen de la disponibilidad del sustrato xolu (Spec 006) para el endpoint `GET /schemas`.

| Variable | Tipo | Default | Valores válidos | Justificación operativa |
|----------|------|---------|-----------------|------------------------|
| `MOLU_FRONT_SCHEMA_REFRESH_INTERVAL` | duration | `5m` | `10s` – `24h` | Intervalo hot-reload de schemas. 5m balancea frescura vs carga en xolu. Impulsado por ticker SIGHUP/periodic (RF-4). |
| `MOLU_FRONT_SCHEMA_TIMEOUT` | duration | `10s` | `1s` – `60s` | Timeout para GET `/schemas`. Utilizado en reintentos RF-3. |
| `MOLU_FRONT_SCHEMA_RETRY_FLOOR` | duration | `1s` | `100ms` – `60s` | Backoff mínimo al fallar carga schemas. Parte del backoff exponencial RF-3. |
| `MOLU_FRONT_SCHEMA_RETRY_CEILING` | duration | `30s` | `1s` – `3600s` | Backoff máximo schemas. Parte del backoff exponencial RF-3. |
| `MOLU_FRONT_SCHEMA_MAX_ATTEMPTS` | int | `10` | `1` – `100` | Reintentos máximos carga inicial antes de fallar arranque. RF-3 especifica `maxAttempts` en `Load()`. |

---

## Variables RPI (Spec 004)

Controlan la política de admisión y scoring del **Eje 1 de control** (qué se ofrece al agente). Estas variables definen el proveedor RPI y sus reglas de filtrado, según la interfaz RPI definida en la Spec 004. Son independientes del dominio y respetan la neutralidad de requisito RF-10.

| Variable | Tipo | Default | Valores válidos | Justificación operativa |
|----------|------|---------|-----------------|------------------------|
| `MOLU_FRONT_RPI_PROVIDER` | string | `admit-all` | `admit-all`, `static-context-rules` | Selecciona la política de RPI. `admit-all` para desarrollo; `static-context-rules` para producción con reglas YAML/JSON. RF-1, RF-7. |
| `MOLU_FRONT_RPI_RULES_FILE` | string | `""` | Ruta a archivo JSON/YAML | Ruta externa a archivo de reglas de admisión. Si se provee, se carga al arranque y se utiliza con `MOLU_FRONT_RPI_PROVIDER=static-context-rules`. |
| `MOLU_FRONT_RPI_RULES_JSON` | string | `""` | JSON inline de reglas | Regras de admisión en formato JSON incrustado en la variable de entorno. Alternativa a `MOLU_FRONT_RPI_RULES_FILE` para desarrollo rápido. RF-6, RF-7. |

---

## Variables de Hub (Spec 005)

| Variable | Tipo | Default | Valores válidos | Justificación operativa |
|----------|------|---------|-----------------|------------------------|
| `MOLU_HUB_PORT` | int | `8081` | `1` – `65535` | Puerto HTTP para el Hub Registry. Debe ser distinto del puerto MCP frontal. |
| `MOLU_HUB_RPI_PROVIDER` | string | `admit-all` | `admit-all`, `static-context-rules` | Proveedor RPI para el Hub. `admit-all` para desarrollo; `static-context-rules` con reglas YAML en producción. RF-1. |
| `MOLU_HUB_STATIC_RULES_PATH` | string | `""` | Ruta a archivo YAML | Ruta a archivo de reglas estáticas (tenant/role → tools permitidos). Requerido si `MOLU_HUB_RPI_PROVIDER=static-context-rules`. RF-6. |
| `MOLU_HUB_TRANSPORT` | string | `stdio` | `stdio`, `streamable-http` | Transporte de comunicación del Hub. `stdio` para despliegue local; `streamable-http` para integración con servicios externos. RF-11. |
| `MOLU_HUB_HTTP_ADDR` | string | `:8091` | Dirección HTTP válida | Dirección y puerto HTTP del servidor Hub. Default `:8091` cuando `transport=streamable-http`. |
| `MOLU_HUB_HTTP_AUTH` | string | `none` | `none`, `bearer`, `mtls` | Mecanismo de autenticación HTTP para el Hub. `none` por defecto en entornos aislados. |
| `MOLU_HUB_HTTP_BEARER_TOKEN_FILE` | string | `""` | Ruta a archivo token | Archivo que contiene el token Bearer para autenticación `bearer`. Debe ser legible por el proceso Hub. |
| `MOLU_HUB_CALL_TIMEOUT` | duration | `30s` | `1s` – `300s` | Timeout de llamadas MCP entrantes al Hub. Protege contra colas bloqueantes. |
| `MOLU_HUB_LOG_LEVEL` | string | `info` | `debug`, `info`, `warn`, `error` | Nivel de log del Hub. `debug` en desarrollo; `info` en producción. |
| `MOLU_HUB_LOG_FORMAT` | string | `console` | `console`, `json` | Formato de salida de logs del Hub. `console` para desarrollo; `json` para agregadores estructurados. |

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
MOLU_FRONT_RPI_PROVIDER=admit-all
```

### Staging / CI
```bash
MOLU_FRONT_LOG_FORMAT=json
MOLU_FRONT_LOG_LEVEL=info
MOLU_FRONT_LOG_OUTPUT_PATH=/var/log/molu
MOLU_FRONT_TELEMETRY_ENABLED=true
MOLU_FRONT_TELEMETRY_PORT=9090
MOLU_FRONT_RPI_PROVIDER=static-context-rules
MOLU_HUB_PORT=8081
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
MOLU_FRONT_RPI_PROVIDER=static-context-rules
MOLU_HUB_PORT=8081
MOLU_HUB_RPI_PROVIDER=static-context-rules
MOLU_HUB_STATIC_RULES_PATH=/etc/molu/rules.yaml
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
7. **Valores RPI**: `MOLU_FRONT_RPI_PROVIDER` debe ser `admit-all` o `static-context-rules`
8. **Reglas RPI**: Si `MOLU_FRONT_RPI_PROVIDER=static-context-rules`, entonces `MOLU_FRONT_RPI_RULES_FILE` o `MOLU_FRONT_RPI_RULES_JSON` debe estar configurado
9. **Valores Hub**: `MOLU_HUB_PORT` en rango `1-65535`, `MOLU_HUB_LOG_LEVEL` en niveles válidos
10. **Formato transport**: `MOLU_HUB_TRANSPORT` debe ser `stdio` o `streamable-http`

**Error de validación** → Log nivel ERROR + exit code 1.

---

## Referencia Rápida (Cheatsheet)

```bash
# Ver variables actuales
env | grep MOLU_FRONT | sort

# Ver defaults efectivos
molu --print-config
```