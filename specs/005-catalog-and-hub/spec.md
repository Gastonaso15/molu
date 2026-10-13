# Spec 005 — Catálogo y Hub/Registry MCP (Eje 1: Qué se ofrece)

Estado: borrador

## Contexto y objetivo
Esta especificación define el **Hub/Registry MCP** como servicio independiente (según entregables §3.1) que actúa como **catálogo desacoplado**: registra funciones publicadas por publicadores externos, expone una superficie MCP para descubrimiento (`tools/list`, `tools/call`), y aplica la interfaz RPI (`Admit`/`Score`) para filtrar qué se ofrece a cada agente según tenant/rol. El Hub es **agnóstico de dominio** (neutralidad estricta: cero referencias a "Seam") y stateless en persistencia propia (catálogo en memoria).

## Usuarios / Actores
- **Publicador externo**: Registra funciones de dominio (`namespace.Nombre`) y/o primitivos genéricos (`walk`, `find`, `get`) via herramientas MCP del Hub.
- **Agente de IA (cliente MCP)**: Consume `tools/list` y `tools/call` del Hub para descubrir y ejecutar herramientas admitidas.
- **molu (frontal MCP)**: Puede consumir el Hub como fuente de catálogo (integración futura) o mantener catálogo propio — spec 005 define Hub standalone.
- **Operador**: Configura Hub via variables `MOLU_HUB_*`, selecciona proveedor RPI.

## Historias de usuario
- HU-1: Como **publicador**, quiero registrar mis funciones (`work_orders.CreateOrder`, `inventory.ReserveStock`) y primitivos (`walk`) en el Hub via MCP, para que estén disponibles para agentes.
- HU-2: Como **agente**, quiero hacer `tools/list` en el Hub y recibir solo herramientas admitidas para mi tenant/rol (RPI Admit/Score), para no ver funciones fuera de alcance.
- HU-3: Como **operador**, quiero configurar el proveedor RPI del Hub (`admit-all` | `static-context-rules`) por variable de entorno, para cambiar política de admisión sin recompilar.
- HU-4: Como **publicador**, quiero actualizar/eliminar mis funciones registradas, para mantener catálogo vigente.

## Ejes de Control Involucrados
- **Eje 1 (Qué se ofrece)**: **Principal y exclusivo**. El Hub registra, admite (RPI Admit), scorea (RPI Score) y expone catálogo filtrado via MCP. No ejecuta FSMs.
- **Eje 2 (Qué puede ejecutarse)**: **Fuera de alcance**. La ejecución (`walk`, transiciones FSM) pertenece a molu/xolu. El Hub solo expone herramientas; la invocación real puede redirigir a molu o ser stub en V1.

## Convención de Nombres de Herramienta
- **Ambos tipos**: El Hub registra y expone indistintamente:
  - **Primitivos genéricos**: NUNCA llevan namespace (ej. `walk`, `find`, `get`).
  - **Funciones de dominio**: SIEMPRE llevan `namespace.Nombre` (ej. `work_orders.CreateOrder`, `inventory.ReserveStock`).
- El Hub valida que las funciones de dominio tengan namespace no vacío y los primitivos no lo tengan.

## Definiciones
- **Publicador**: Entidad externa que registra herramientas en el Hub. Identificado por `publisher_id` (string).
- **Registro de herramienta (`ToolRegistration`)**: `{ publisher_id, name, description, inputSchema, primitive: bool, namespace: string, metadata: map }`.
- **Catálogo en memoria (`Registry`)**: Mapa `publisher_id -> []ToolRegistration` + índice plano `name -> ToolRegistration` para lookup rápido.
- **Contexto de admisión (`AdmitContext`)**: `{ tenant: string, roles: []string, query?: string, publisher_id?: string }`.
- **Proveedor RPI (`RPIProvider`)**: Interfaz compartida con molu (spec 004): `Admit(ctx, context, candidates) -> []ToolCandidate`, `Score(ctx, context, admitted) -> []ScoredTool`.
- **Herramientas MCP expuestas por el Hub**:
  - `registry.register` — registra herramienta (params: ToolRegistration)
  - `registry.unregister` — elimina herramienta (params: `{ publisher_id, name }`)
  - `registry.list` — lista herramientas del publicador (params: `{ publisher_id? }`)
  - `tools/list` — descubrimiento filtrado por RPI (params: AdmitContext)
  - `tools/call` — invocación (V1: stub que retorna NOT_OFFERED "execution not implemented"; futura: proxy a molu/xolu)

## Requisitos funcionales (en notación EARS)
- RF-1: CUANDO el Hub arranca, EL SISTEMA carga proveedor RPI indicado en `MOLU_HUB_RPI_PROVIDER` (`admit-all` | `static-context-rules`).
- RF-2: CUANDO un publicador invoca `registry.register`, EL SISTEMA valida payload (schema JSON, convención nombres: primitivo sin namespace, dominio con namespace) y almacena en catálogo; SI payload inválido, ENTONCES retorna **CONTRACT_VIOLATION** con `details.fields[]`.
- RF-3: CUANDO un publicador invoca `registry.unregister`, EL SISTEMA elimina la herramienta si existe; SI no existe, ENTONCES retorna **NOT_FOUND** con `details: { publisher_id, name }`.
- RF-4: CUANDO un agente invoca `tools/list` con `AdmitContext`, EL SISTEMA invoca `Admit` sobre catálogo completo y expone SOLO herramientas admitidas; SI lista admitida vacía, ENTONCES retorna respuesta vacía (código **EMPTY**).
- RF-5: SI una herramienta existe en catálogo pero `Admit` la excluye para el tenant/rol, ENTONCES EL SISTEMA NO la expone en `tools/list` (omisión = **NOT_OFFERED** en auditoría, no se retorna al cliente).
- RF-6: CUANDO se expone lista admitida, EL SISTEMA invoca `Score` para ordenar por relevancia descendente.
- RF-7: CUANDO un agente invoca `tools/call` para herramienta admitida, EL SISTEMA (V1) retorna **NOT_OFFERED** con mensaje "execution not implemented in Hub; use molu frontal" — ejecución real es responsabilidad de molu.
- RF-8: CUANDO un cliente consulta `registry.list` sin `publisher_id`, EL SISTEMA retorna todas las herramientas registradas (sin filtro RPI — es vista administrativa).
- RF-9: SI `AdmitContext.tenant` está vacío en `tools/list`, ENTONCES EL SISTEMA retorna **CONTRACT_VIOLATION** con `details.fields: ["tenant"]`.
- RF-10: EL SISTEMA garantiza concurrencia segura: registros, unregistros, y `tools/list` concurrentes no producen data races (`go test -v -race ./...` pasa).
- RF-11: EL SISTEMA mantiene neutralidad de dominio: cero referencias a "Seam" en código, constantes, defaults, configuración.

## Requisitos no funcionales
- **Concurrencia segura**: cero data races bajo `go test -v -race ./...`.
- **Neutralidad de dominio**: prohibido "Seam" en código, constantes, variables, paquetes, defaults.
- **Configurabilidad**: proveedor RPI, puerto, timeouts via `MOLU_HUB_*`.
- **Rendimiento**: `tools/list` (Admit+Score) < 10ms para catálogo < 500 herramientas.
- **Compatibilidad**: Go 1.27+, SDK MCP oficial, transporte stdio y streamable-http.
- **Despliegue**: binario único, orden tolerado (Hub antes que molu si molu lo consume), reintentos backoff.

## Casos límite
- Catálogo vacío al arranque: `tools/list` → EMPTY; `registry.list` → array vacío.
- Publicador registra herramienta con nombre duplicado (mismo publisher_id): sobrescribe (update) o error — decidir en plan.
- `static-context-rules` sin reglas para tenant/rol: denegar todo → EMPTY en `tools/list`.
- Nombres de herramienta con Unicode, case-sensitivity: matching exacto.
- `tools/call` para herramienta no admitida (no en lista Admit): NOT_OFFERED.

## Fuera de alcance
- **Persistencia duradera** (DB, etcd, archivos) — catálogo solo en memoria, reinicio pierde registry.
- **Autenticación/autorización de publicadores** — confianza total, solo tenant/rol en AdmitContext.
- **Clustering/HA** — instancia única, sin liderazgo ni replicación.
- **UI/admin para gestionar publicadores** — gestión solo via herramientas MCP (`registry.register`, `unregister`, `list`).
- **Sincronización automática con molu** — molu y Hub independientes; cada uno carga su catálogo (molu via schemas xolu, Hub via publicaciones).
- **Telemetría/métricas** (Prometheus, OpenTelemetry) — solo logs estructurados.
- **Publicadores externos remotos** (gRPC, HTTP) — registro solo via MCP local (stdio/streamable-http).
- **Ejecución real de herramientas** (`tools/call` proxy a xolu/molu) — V1 retorna NOT_OFFERED "execution not implemented".

## Criterios de finalización
- Todos los RF cubiertos con tests unitarios en verde (`go test -v -race ./...`).
- Pruebas de integración dedicadas para **NOT_OFFERED**, **NOT_FOUND**, **EMPTY**, **CONTRACT_VIOLATION**, **SUBSTRATE_UNAVAILABLE** (Criterio 2).
- Test de integración que demuestra cambio de proveedor RPI por config altera descubrimiento sin tocar código (Criterio 7).
- Diagrama de secuencia Mermaid (`.mmd` y `.pdf`) para flujos: registro publicador → `tools/list` (Admit+Score) → respuesta; `tools/call` stub; `registry.list` admin.

## Dudas resueltas
- [RESUELTO] **Comportamiento ante registro duplicado (`publisher_id` + `name`)**: Sobrescribe atómicamente de forma idempotente sin emitir error (según §4.2 de la spec del Hub), permitiendo que los publicadores actualicen sus definiciones o se recuperen de reinicios.
- [RESUELTO] **Formato de reglas para `static-context-rules` en Hub**: Mismo formato JSON (`map[string][]string`) e infraestructura compartida que molu (Spec 004 vía `pkg/rpi/`), cargado desde archivo vía `MOLU_HUB_RPI_RULES_FILE` o inline vía `MOLU_HUB_RPI_RULES_JSON`.
- [RESUELTO] **Comportamiento de `tools/call` en Hub V1**: Retorna error tipado `NOT_OFFERED` fijo indicando que la ejecución no está soportada en el Hub y debe ser dirigida al frontal molu, respetando la estricta separación de roles (§2: el Hub solo media en descubrimiento, nunca en invocación).
- [RESUELTO] **Variables de entorno `MOLU_HUB_*`**:
  - `MOLU_HUB_TRANSPORT`: `stdio` | `streamable-http` (default: `stdio`)
  - `MOLU_HUB_HTTP_ADDR`: Dirección y puerto HTTP (default: `:8091`)
  - `MOLU_HUB_HTTP_AUTH`: `none` | `bearer` | `mtls` (default: `none`)
  - `MOLU_HUB_HTTP_BEARER_TOKEN_FILE`: Archivo con token Bearer
  - `MOLU_HUB_RPI_PROVIDER`: `admit-all` | `static-context-rules` (default: `admit-all`)
  - `MOLU_HUB_RPI_RULES_FILE`: Archivo JSON de reglas
  - `MOLU_HUB_RPI_RULES_JSON`: JSON inline de reglas
  - `MOLU_HUB_CALL_TIMEOUT`: Timeout de llamadas MCP (default: `30s`)
  - `MOLU_HUB_LOG_LEVEL`: `debug` | `info` | `warn` | `error` (default: `info`)
  - `MOLU_HUB_LOG_FORMAT`: `console` | `json` (default: `console`)