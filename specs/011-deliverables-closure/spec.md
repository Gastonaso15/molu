# Spec 011 — Cierre de Brechas de Auditoría y Entregables Normativos

Estado: aprobada

## Contexto y objetivo
La auditoría integral del repositorio (19-nov-2026) frente al documento normativo *SEAM-CURE — Equipo IA / MCP: Detalle de Entregables* (12 de septiembre de 2026) confirmó una base de código sólida y sin carreras de datos, pero identificó brechas normativas puntuales que impiden considerar cerrada la entrega formal: (1) documentación incompleta en despliegue de los 4 procesos, desfase en `configuration.md`, cobertura parcial de taxonomía en `runbook.md` y ausencia total de `.pdf` renderizados y diagramas de secuencia de las funciones de Work Orders (§2.3.1); (2) comportamiento incompleto en `molu` respecto a superficie unificada en `tools/list` (§0.5, §2.1), sondeo/retención de catálogo del Hub (`MOLU_FRONT_CATALOGUE_POLL_INTERVAL`) y transporte `streamable-http`; (3) falta de telemetría en el binario standalone `cmd/hub` (§2.3) y detalles de contrato/extensibilidad en RPI (§2.5).

Esta especificación formaliza el cierre completo y verificable de dichas brechas para alcanzar el 100% de cumplimiento del piso normativo del proyecto.

## Usuarios / Actores
- **Agente de IA (Cliente MCP)**: Consume una superficie unificada (primitivos genéricos + funciones de dominio) sobre `stdio` o `streamable-http`.
- **Operador / SRE**: Despliega los 4 procesos (`xolu`, `hub`, `publisher-orders`, `molu`), configura variables de entorno, consulta métricas/logs de todo el circuito y sigue el runbook operativo.
- **Evaluador / Revisor de Entregables**: Verifica el cumplimiento normativo de código, pruebas de integración, diagramas (`.mmd` + `.pdf`) y documentación en texto plano.
- **Equipo de dominio / Futuro desarrollador de RPI**: Consulta la nota de diseño de extensibilidad de `Score` y el test double de solo-puntuación para integrar futuros motores semánticos.

## Historias de usuario
- **HU-1**: Como **agente de IA**, quiero que `tools/list` en `molu` me presente en una sola lista unificada tanto los primitivos genéricos (`walk`, `get`, `list`, etc.) como las funciones de dominio admitidas por el Hub (`work_orders.*`), para operar sobre ambos ejes de control simultáneamente.
- **HU-2**: Como **operador**, quiero que `molu` sondee periódicamente el catálogo del Hub y retenga el último catálogo válido si el Hub sufre un corte transitorio, para evitar degradar innecesariamente la superficie del agente.
- **HU-3**: Como **operador**, quiero poder levantar `molu` en modo `streamable-http` cuando `MOLU_FRONT_TRANSPORT=streamable-http`, para desplegarlo detrás de un balanceador o en red sin depender de `stdio`.
- **HU-4**: Como **operador / SRE**, quiero que `cmd/hub` emita logs estructurados, métricas Prometheus y propague correlación (`X-Request-ID`, `traceparent`), para auditar el circuito completo de extremo a extremo.
- **HU-5**: Como **arquitecto / evaluador**, quiero que cada proveedor RPI declare qué etapas implementa (`Admit`/`Score`) y contar con una nota de diseño y un test double de solo-puntuación, para verificar la extensibilidad de §2.5 sin tocar la interfaz.
- **HU-6**: Como **operador / evaluador**, quiero contar con la documentación completa y sincronizada (despliegue de 4 procesos, configuración real, runbook de los 6 códigos de taxonomía) y todos los diagramas de secuencia en `.mmd` y `.pdf` (incluidas todas las funciones de Work Orders), para operar y validar el sistema sin dependencias externas.

## Ejes de Control Involucrados
- **Eje 1 (Qué se ofrece)**: Reforzado mediante la declaración explícita de etapas en `RPIProvider`, el logging de `Score` en `hub.Discovery`, la superficie unificada en `molu` (sin ocultar primitivos genéricos), la retención de catálogo ante cortes del Hub y la protección contra colisiones con nombres de primitivos genéricos.
- **Eje 2 (Qué puede ejecutarse)**: Garantizado de forma continua al mantener siempre accesibles los primitivos genéricos (`walk`, `machine_state`, etc.) junto a las funciones de dominio y documentando sus flujos de rechazo verbatim en los diagramas de secuencia y en el runbook operativo.

## Convención de Nombres de Herramienta
- **Primitivos genéricos**: NUNCA llevan namespace (`walk`, `get`, `list`, `create`, `update`, `query`, `describe`, `machine_state`, `machine_history`, `cal_*`). En caso de colisión con una función publicada, el primitivo genérico gana siempre (§2.6).
- **Funciones de dominio**: SIEMPRE llevan `namespace.Nombre` (e.g. `work_orders.CreateOrder`, `work_orders.GetOrder`, `work_orders.AdvanceOrder`, `work_orders.ListTemplates`).

## Definiciones
- **Superficie unificada (`tools/list`)**: Unión determinista de los primitivos genéricos activos en `molu` y las funciones de dominio admitidas por el Hub/RPI para el contexto del solicitante.
- **Retención de catálogo (*stale-while-error*)**: Mecanismo por el cual `RemoteHubClient` conserva en memoria la última instantánea válida del catálogo del Hub si un sondeo periódico falla, registrando el fallo en el log de red sin vaciar la superficie.
- **Capacidades RPI (`RPICapabilities`)**: Estructura `{ SupportsAdmit bool, SupportsScore bool }` declarada por cada `RPIProvider`.
- **Test double de solo-puntuación**: Implementación mínima de `RPIProvider` orientada exclusivamente a ejercitar la etapa `Score` (ordenamiento por relevancia simulada) respetando todas las garantías de §2.5.

## Requisitos funcionales (en notación EARS)

### Bloque A — Frontal MCP `molu` (Superficie unificada, retención de catálogo y transporte HTTP)
- **RF-1 (Superficie unificada en `tools/list`)**: CUANDO un cliente MCP invoca `tools/list` en `molu` con el Hub conectado y un `tenant` válido (explícito o vía `DefaultTenant`), EL SISTEMA retorna la superficie unificada compuesta por los 13 primitivos genéricos activos más las funciones de dominio admitidas por el Hub/RPI; SI el Hub retorna `EMPTY` (sin funciones de dominio admitidas), ENTONCES EL SISTEMA devuelve igualmente los 13 primitivos genéricos; SI el `tenant` está vacío y no hay `DefaultTenant`, ENTONCES retorna `CONTRACT_VIOLATION` (`details.fields: ["tenant"]`); SI existe colisión de nombre entre una función del Hub y un primitivo genérico, ENTONCES EL SISTEMA descarta la función colisionante y conserva el primitivo genérico.
- **RF-2 (Sondeo periódico y retención de catálogo)**: MIENTRAS `molu` corre conectado a un Hub remoto (`MOLU_FRONT_HUB_URL`), EL SISTEMA sondea periódicamente el catálogo según `MOLU_FRONT_CATALOGUE_POLL_INTERVAL`; SI un sondeo falla por indisponibilidad del Hub, ENTONCES EL SISTEMA registra el error en el log de red y retiene el último catálogo válido conocido en caché. SI nunca hubo un catálogo válido y el Hub es inalcanzable al consultar discovery, ENTONCES retorna `SUBSTRATE_UNAVAILABLE`.
- **RF-3 (Transporte `streamable-http` completo)**: CUANDO `molu` arranca con `MOLU_FRONT_TRANSPORT=streamable-http`, EL SISTEMA expone el servidor MCP sobre HTTP en `MOLU_FRONT_HTTP_ADDR` (default `:8090`) soportando peticiones POST JSON-RPC 2.0 y respuestas en streaming SSE (`text/event-stream` / streamable HTTP según `Accept` y notificaciones `notifications/tools/list_changed`), aplicando la autenticación configurada en `MOLU_FRONT_HTTP_AUTH` (`none` | `bearer` con `MOLU_FRONT_HTTP_BEARER_TOKEN_FILE`) y la misma taxonomía tipada que `stdio`.

### Bloque B — Hub Standalone y Telemetría de Circuito Completo
- **RF-4 (Telemetría en `cmd/hub`)**: CUANDO `cmd/hub` arranca, EL SISTEMA inicializa el subsistema `pkg/telemetry` (logs estructurados de 4 dominios, métricas Prometheus, `/healthz` y propagación de `X-Request-ID`/`traceparent` en cada request HTTP de registro y descubrimiento).
- **RF-5 (Protección contra colisión de nombres reservados)**: CUANDO un publicador intenta registrar en el Hub una herramienta con un nombre reservado de primitivo genérico sin namespace o que colisione con el conjunto de primitivos estándar, EL SISTEMA rechaza el registro retornando `CONTRACT_VIOLATION` con `details.fields: ["name"]`.
- **RF-6 (Auditoría de etapa `Score`)**: CUANDO `hub.Discovery` ejecuta la etapa de puntuación, EL SISTEMA invoca `rpi.ScoreWithLogging` para registrar latencia y resultado de puntuación de forma consistente con `AdmitWithLogging`.
- **RF-14 (Autorización de publicadores por namespace, §2.6)**: CUANDO `cmd/hub` está configurado con reglas de autorización de publicadores (`MOLU_HUB_PUBLISHER_TOKENS_JSON` o `MOLU_HUB_PUBLISHER_TOKENS_FILE`, mapa `token -> []namespace`), EL SISTEMA exige `Authorization: Bearer <token>` en `/api/v1/registry/register` y `/api/v1/registry/unregister` y verifica que el `namespace` de la herramienta esté autorizado para ese token; SI falta el token o es inválido retorna HTTP 401; SI el token es válido pero el `namespace` no está autorizado retorna HTTP 403 con `NOT_OFFERED` (o `CONTRACT_VIOLATION` / error de autorización tipado). SI no se configura ningún mapa de tokens, el Hub opera en modo abierto (retrocompatible para desarrollo local).

### Bloque C — Interfaz RPI y Extensibilidad Demostrada (§2.5)
- **RF-7 (Declaración de etapas RPI)**: EL SISTEMA extiende `rpi.RPIProvider` con `Capabilities() rpi.Capabilities` (indicando `Admit` y `Score`), declarando `{Admit: true, Score: false}` en `admit-all` y `static-context-rules`.
- **RF-8 (Extensibilidad de `Score` demostrada)**: EL SISTEMA incluye una nota de diseño en `docs/rpi-extensibility.md` y un test double de solo-puntuación (`pkg/rpi/score_double_test.go`) que demuestra que un motor de puntuación encaja en la etapa `Score` cumpliendo las garantías de §2.5 (referencias, partición previa por tenant, sin padding, error tipado si no está disponible, sin tipos internos expuestos) sin modificar la interfaz `RPIProvider`.

### Bloque D — Documentación Normativa Obligatoria (§2.3.1) y ADRs
- **RF-9 (Guía de despliegue de 4 procesos)**: EL SISTEMA entrega `docs/deployment.md` documentando cómo levantar los 4 procesos (`xolu`, `hub`, `publisher-orders`, `molu`), el orden de arranque tolerado con backoff (`MOLU_FRONT_STARTUP_MAX_ATTEMPTS`), qué expone cada proceso en red (`stdio` y `streamable-http`), y el procedimiento de verificación post-arranque de conectividad entre los cuatro.
- **RF-10 (Configuración exhaustiva sincronizada)**: EL SISTEMA actualiza `docs/configuration.md` para listar el 100% de las variables efectivas en código (`MOLU_FRONT_*`, `MOLU_HUB_*`, `MOLU_PUBLISHER_*`), corrigiendo todos los defaults divergentes respecto a `pkg/config/config.go` y añadiendo una frase operativa sobre cuándo conviene modificar cada variable.
- **RF-11 (Runbook operativo completo)**: EL SISTEMA amplía `docs/runbook.md` para cubrir el diagnóstico operativo de los 6 códigos de taxonomía de §2.4 (`NOT_OFFERED`, `NOT_FOUND`, `EMPTY`, `INVALID_TRANSITION`, `CONTRACT_VIOLATION`, `SUBSTRATE_UNAVAILABLE`) más `XOLU-MOLU-FRONT-TIMEOUT`, detallando la distinción entre corte transitorio y falla manual de xolu, qué revisar cuando el catálogo del Hub deja de actualizarse, y corrigiendo enlaces rotos.
- **RF-12 (Diagramas de secuencia propios en `.mmd` + `.pdf`)**: EL SISTEMA entrega en `docs/diagrams/` los archivos fuente `.mmd` y su correspondiente `.pdf` renderizado (`-b white`) para:
  - Cada función de dominio publicada en el piloto de Work Orders (`work_orders.CreateOrder`, `work_orders.GetOrder`, `work_orders.AdvanceOrder`, `work_orders.ListTemplates`).
  - Los flujos pendientes de `molu` MCP (`006-mcp-server-sequence`) y Execution Bridge (`008-execution-bridge-sequence`).
  - El renderizado `.pdf` de todos los diagramas `.mmd` ya existentes (`001`, `002`, `003`, `007`).
- **RF-13 (ADRs de decisiones arquitectónicas)**: EL SISTEMA documenta en `MEMORY.md` los ADRs que argumentan formalmente las decisiones tomadas bajo el margen prescriptivo del documento: (a) implementación directa de JSON-RPC 2.0 en `pkg/mcp` vs `go-sdk`; (b) rutas HTTP REST del Hub (`/api/v1/registry/*` y `/api/v1/discovery/tools`) vs `POST /publish` y `GET /catalogue`.

> Nota sobre la taxonomía: Nunca colapsar los resultados en errores genéricos. Preservar estrictamente los 6 códigos tipados (§2.4): `NOT_OFFERED`, `NOT_FOUND`, `EMPTY`, `INVALID_TRANSITION`, `CONTRACT_VIOLATION`, `SUBSTRATE_UNAVAILABLE`.

## Requisitos no funcionales
- **Concurrencia segura**: Cero data races bajo `go test -v -race ./...` en todos los paquetes, incluido el ticker de sondeo de catálogo y el transporte HTTP concurrente.
- **Neutralidad de dominio estricta**: Cero ocurrencias de la palabra restringida en código, constantes o defaults de `molu`, `hub`, `bridge`, `rpi`, `config`, `telemetry` y `taxonomy`.
- **Calidad estática**: `go vet ./...` sin advertencias y `make build` compilando los 4 binarios sin errores.
- **Formato documental**: Documentación en Markdown puro (texto plano versionado) y diagramas en `.mmd` + `.pdf` con fondo blanco (`-b white`).

## Casos límite
- **Catálogo del Hub vacío con primitivos activos**: `tools/list` en `molu` devuelve los 13 primitivos genéricos sin fallar con `EMPTY` global (si el Hub responde `EMPTY` para funciones de dominio, los primitivos siguen siendo utilizables).
- **Hub caído al arrancar `molu` vs. caído en caliente**: Si el Hub ya entregó una instantánea previa, se retiene y se loguea el fallo de sondeo; si nunca respondió, `Lookup` no ofrece funciones de dominio y discovery reporta el fallo apropiadamente.
- **Colisión de nombre en `tools/list`**: Si un Hub devolviera una herramienta llamada `walk`, `molu` prioriza su primitivo genérico `walk`.
- **Entorno sin Chromium para `mmdc`**: Si `mmdc` falla por falta de Chromium, documentar/usar la configuración de Puppeteer (`-p`) o navegador disponible del sistema para garantizar los `.pdf` renderizados.

## Fuera de alcance
- Migración de `pkg/mcp/server.go` al SDK externo `github.com/modelcontextprotocol/go-sdk` (se documenta como ADR).
- Implementación de un motor real de búsqueda semántica/vectorial en RPI (solo nota de diseño + test double según §2.5).
- Nuevas funciones de dominio en Work Orders (`ClaimOrder`/backlog, bloques, almacén móvil) — ya documentadas como evolución futura en ADR-012.

## Criterios de finalización
- Todos los RF (RF-1 a RF-14) implementados y cubiertos con tests en verde bajo `go test -v -race ./...`.
- `go vet ./...` y `make build` (4 binarios) exitosos.
- Verificación de neutralidad (`grep -rni seam cmd/molu cmd/hub pkg/bridge pkg/config pkg/exec pkg/hub pkg/mcp pkg/rpi pkg/schema pkg/taxonomy pkg/telemetry pkg/xolu`) con 0 coincidencias en código productivo.
- Todos los documentos de §2.3.1 presentes, sincronizados y sin enlaces rotos (`docs/deployment.md`, `docs/configuration.md`, `docs/runbook.md`, `docs/rpi-extensibility.md`).
- Todos los diagramas `.mmd` en `docs/diagrams/` con su par `.pdf` renderizado y verificable.
- `MEMORY.md` actualizado con el cierre de la Spec 011 y los nuevos ADRs.

## Dudas resueltas
- [RESUELTO] **Transporte `streamable-http` (RF-3)**: Se incluye la implementación completa en esta spec (POST JSON-RPC 2.0 + SSE/streamable HTTP en `cmd/molu` con auth `none`/`bearer`).
- [RESUELTO] **`tools/list` unificado con Hub en `EMPTY` (RF-1)**: Cuando el `tenant` es válido (explícito o vía `DefaultTenant`) y el Hub responde `EMPTY`, `molu` devuelve los 13 primitivos genéricos; si el `tenant` está vacío sin `DefaultTenant`, sigue devolviendo `CONTRACT_VIOLATION` (`details.fields: ["tenant"]`).
- [RESUELTO] **Autorización de publicadores por namespace (§2.6, RF-14)**: Se incluye un mapa simple `token -> []namespace` en `cmd/hub` configurable por env/JSON (`MOLU_HUB_PUBLISHER_TOKENS_JSON` / `MOLU_HUB_PUBLISHER_TOKENS_FILE`), manteniendo modo abierto si no está configurado.
