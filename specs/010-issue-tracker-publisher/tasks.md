# Tareas: Spec 010 — Publicador de Issue Tracker

## Resumen de Tareas
- [x] **T1. Dominio y Operaciones de Issue Tracker (`pkg/publisher/issues/service.go`).** RF-1, RF-2, RF-3, RF-4, RF-7, RF-9, RF-10
  - **Archivos**: `pkg/publisher/issues/types.go`, `pkg/publisher/issues/service.go`, `pkg/publisher/issues/service_test.go`
  - **Descripción**: Definir tipos `Issue`, matriz y cálculo de SLA (primera respuesta y resolución), y servicio que interactúa con xolu (`/api/v1/issues`, `/api/v1/assets`, `/api/v2/fsm/walk`). Implementar `CreateIssue` (calcula SLA, crea máquina FSM `issue_lifecycle` y entidad `issues`), `GetIssue` (404 ante inexistencia) y `AdvanceIssue` (invoca `walk` en FSM; si xolu rechaza la transición, retorna error `XOLU-INVALID_TRANSITION` verbatim).
  - **Criterio de finalización**: `go test -v -race ./pkg/publisher/issues/... -run "TestIssueService"` pasa contra mock de xolu.

- [x] **T2. Servidor HTTP y Middleware de Autenticación (`pkg/publisher/issues/server.go`).** RF-1, RF-2, RF-3, RF-6, RF-7
  - **Archivos**: `pkg/publisher/issues/server.go`, `pkg/publisher/issues/server_test.go`
  - **Descripción**: Servidor HTTP en `:8093` con endpoints `POST /api/v1/issues/create`, `POST /api/v1/issues/advance`, `POST /api/v1/issues/get` y `GET /healthz`. Middleware Bearer valida cabecera `Authorization: Bearer <token>`. Retorna HTTP 401 si token inválido, 404 si incidencia no existe, 409 con sobre xolu si transición rechazada, y 503 si xolu inalcanzable.
  - **Criterio de finalización**: `go test -v -race ./pkg/publisher/issues/... -run "TestIssueServer"` pasa.

- [x] **T3. Auto-registro en el Hub y Ciclo de Vida (`pkg/publisher/issues/registrar.go`).** RF-5, RF-8
  - **Archivos**: `pkg/publisher/issues/registrar.go`, `pkg/publisher/issues/registrar_test.go`
  - **Descripción**: Componente que registra las 3 herramientas (`issues.CreateIssue`, `issues.AdvanceIssue`, `issues.GetIssue`) en el Hub remoto con sus esquemas JSON y metadatos `token_ref: "issues:api-token"`. Incluye reintentos con backoff al arranque y desregistro ante shutdown.
  - **Criterio de finalización**: `go test -v -race ./pkg/publisher/issues/... -run "TestIssueRegistrar"` pasa.

- [x] **T4. Binario del Publicador y Makefile (`cmd/publisher-issues/main.go` & `makefile`).** RF-5, RF-9, RF-10
  - **Archivos**: `cmd/publisher-issues/main.go`, `cmd/publisher-issues/main_test.go`, `makefile`
  - **Descripción**: Binario del publicador leyendo `MOLU_PUBLISHER_ISSUES_*`. Actualizar `makefile` para que `make build` compile `bin/molu`, `bin/hub`, `bin/publisher-orders` y `bin/publisher-issues`.
  - **Criterio de finalización**: `make build` compila los 4 binarios sin errores y `grep -ri seam cmd/ pkg/` da cero coincidencias.

- [x] **T5. Prueba de Integración E2E para Issue Tracker (`pkg/bridge_integration/issues_e2e_test.go`).** RF-1 a RF-10
  - **Archivos**: `pkg/bridge_integration/issues_e2e_test.go`
  - **Descripción**: Prueba integral donde un cliente MCP consulta tools via stdio a Molu $\rightarrow$ Molu consulta Hub remoto $\rightarrow$ Publisher de issues auto-registrado atiende llamadas de dominio despachadas por el Execution Bridge $\rightarrow$ se verifica la creación con SLA y el rechazo verbatim de transición inválida.
  - **Criterio de finalización**: `go test -v -race ./pkg/bridge_integration/... -run "TestIssues_E2E"` pasa exitosamente.


---

## Ajustes de alineación con el módulo real de incidencias de Seam (T6–T14)

**Origen**: prueba de punta a punta del 20-nov-2026 con xolu 0.31.0, Seam (ramas `work_orders`, `issue_tracker`, `main` de seam-CURE y la versión 0.4.5), el Hub y ambos publicadores. Work Orders pasa completo; Issue Tracker funciona en los flujos básicos (molu crea y Seam avanza, Seam crea y molu avanza) pero el publicador se escribió sin leer el código real de Seam y difiere en las reglas y el documento que guarda. Mismo caso que ADR-011 en la Spec 009.

**Referencia de Seam** (0.4.5): `internal/ui/issues.go` (`IssueCreate`, `IssueTransition`, `IssueReject`, `ensureIssueFSMDefinition`, tablas SLA, `canActOnIssue`) y `internal/ui/issue_filters.go` (canales).

**Cómo verificar cada tarea**: además de los tests unitarios, repetir la prueba de interoperabilidad contra xolu y Seam reales (el mismo escenario del 20-nov: molu crea y Seam avanza, Seam crea y molu avanza, comparación de documentos). Conviene dejarla como prueba opcional con `MOLU_REAL_XOLU_URL`, como `pkg/publisher/orders/real_xolu_test.go`. La verificación de neutralidad de la Spec 011 excluye `pkg/publisher/*`, donde el nombre de la definición de la FSM de Seam es un contrato externo y no una dependencia.

### Verificación de punta a punta del 26-nov-2026 (Seam 0.4.5 real)

Cliente MCP (stdio) → `molu` → Hub → `publisher-issues` → xolu 0.31.0 real, y en paralelo Seam 0.4.5 compilado de `SEAM-CURE/seam-v0.4.5-checkpoint` (con `enable_issues: true`; el módulo viene apagado por defecto) contra el mismo xolu. Escenario: asset 1 (criticidad `Alta`) y asset 4 (`Media`), la misma incidencia creada desde molu y desde Seam, avances cruzados, transición inválida, rechazo sin motivo. **16 de 26 comprobaciones pasan.**

| Comprobación | Resultado | Tarea |
|---|---|---|
| molu crea; Seam muestra la página y la avanza (`start`) | ✅ | |
| Seam crea; molu la lee (`GetIssue`) y la avanza (`triage`, `start`) | ✅ | |
| Mismos `state`, `priority`, `severity`; primera respuesta (120 / 480 min) | ✅ | |
| `asset_category` para un activo `Alta`: molu `normal`, Seam `critico` | ❌ | T8 |
| Resolución, activo `Alta`/`high`: molu 960 min, Seam 1440 | ❌ | T9 |
| Resolución, activo `Media`/`medium`: molu 2880 min, Seam 4320 | ❌ | T9 |
| `channel` (Seam `app`) y `assignee` (Seam = quien reporta): molu no los escribe | ❌ | T10 |
| `close` desde `in_progress` → `SUBSTRATE_UNAVAILABLE` en vez de `INVALID_TRANSITION` | ❌ | T6 |
| `reject` sin motivo: Seam lo rechaza; molu lo acepta (`state=rejected`, sin `reject_reason`) | ❌ | T11 |
| `issues.AdvanceIssue` no ofrece `merge` | ❌ | T7 |
| Una sola definición `issue_lifecycle` en xolu: **6 iguales** tras crear 6 incidencias | ❌ | **T14 (nueva)** |

Las filas ❌ confirman, contra Seam real, las diferencias que esta lista ya describía; la última no estaba y es peor que lo descrito en T7.

- [x] **T14. Una definición de FSM por incidencia (corrige un fallo, hallado el 26-nov-2026).**
  - **Archivos**: `pkg/publisher/issues/substrate_http.go` (`extractItems`), `substrate_http_test.go`, `service_test.go`
  - **Problema**: el publicador busca su definición en la respuesta de `GET /api/v2/fsm/def` con `extractItems`, que solo lee la clave `items`. xolu 0.31.0 responde `{"definitions": [...]}`, así que nunca la encuentra y **crea una definición `issue_lifecycle` nueva en cada `CreateIssue`** (6 iguales tras 6 incidencias). Los tests con xolu simulado responden `items` y no lo detectan. `pkg/publisher/orders` no tiene el problema: su `definitionItems` acepta `items`, `data`, `definitions`, `defs` y `results` (0 duplicados tras muchas órdenes).
  - **Hecho cuando**: `extractItems` (o su sustituto) reconoce `definitions`; el simulador de pruebas responde con esa forma; una prueba crea dos incidencias seguidas y comprueba que xolu tiene **una** definición con ese nombre; una prueba contra xolu real (`MOLU_REAL_XOLU_URL`) hace lo mismo. Conviene resolverla junto con T7, que cambia el nombre de la definición. Las definiciones duplicadas que ya existan en un xolu de desarrollo no se borran solas.
  - **Riesgo hoy**: con definiciones repetidas, Seam y xolu siguen funcionando (cada máquina apunta a la suya), pero el catálogo de FSM se llena de copias; el equivalente en Work Orders rompería a Seam (`multiple FSM definitions named`), por eso conviene corregirlo antes de que alguien ejecute el publicador de incidencias de forma continua.

- [x] **T6. Rechazo de transición inválida contra xolu real (corrige un fallo).**
  - **Archivos**: `pkg/publisher/issues/substrate_http.go`, `service.go`, `substrate_http_test.go`, `service_test.go`
  - **Problema**: un `AdvanceIssue` no permitido (por ejemplo `close` desde `in_progress`) devuelve `SUBSTRATE_UNAVAILABLE`. xolu responde con un código `XOLU-FSM…` en un 4xx y el publicador solo reconoce HTTP 409 o el texto `INVALID_TRANSITION`.
  - **Hecho cuando**: toda transición rechazada por la FSM (transición inexistente, guarda) devuelve `XOLU-INVALID_TRANSITION` con `details.xolu_code` y el mensaje de xolu verbatim, igual que `pkg/publisher/orders`; `SUBSTRATE_UNAVAILABLE` queda solo para xolu inalcanzable o 5xx; un 404 de la máquina sigue siendo `NOT_FOUND`. Prueba contra xolu real incluida.

- [x] **T7. Una sola definición de FSM, la de Seam, con `merge`.**
  - **Archivos**: `pkg/publisher/issues/types.go`, `substrate_http.go`, `registrar.go`, sus tests
  - **Problema**: el publicador crea `issue_lifecycle`; Seam usa `seam_issue_lifecycle`. Conviven dos definiciones en xolu y la nuestra no tiene la transición `merge → duplicate` (sí tiene las otras 7). Además, `issue_lifecycle` se duplica en cada incidencia (T14): resolver ambas juntas.
  - **Hecho cuando**: `DefinitionName` es `seam_issue_lifecycle`; si ya existe en xolu se reutiliza, y si no se crea idéntica a la de Seam (8 estados, `firstmatch`, las 8 transiciones, incluida `merge` desde `reported`/`triaged`/`in_progress` hacia `duplicate`); el enum de `transition` en el esquema de `issues.AdvanceIssue` incluye `merge`; no queda ninguna definición `issue_lifecycle` nueva.

- [x] **T8. Lectura de la criticidad del activo y categoría `critico`/`normal`.**
  - **Archivos**: `pkg/publisher/issues/service.go`, `types.go`, tests
  - **Problema**: se compara `criticality == "critical"`, pero Seam guarda `Alta`/`Media`/`Baja` (o `High`/`Medium`/`Low`). Un activo crítico queda como `normal` en molu y como `critico` en Seam.
  - **Hecho cuando**: criticidad `Alta`/`High` produce `asset_category = "critico"`; `Media`/`Baja`/`Medium`/`Low`, vacío, activo ausente o ilegible producen `"normal"`; los valores guardados son `critico`/`normal` (no `critical`). Si el activo no existe, `NOT_FOUND` como hoy.

- [ ] **T9. Tablas SLA idénticas a las de Seam.**
  - **Archivos**: `pkg/publisher/issues/types.go`, `service.go`, tests
  - **Problema**: `DefaultSLAFirstResponseMinutes` y `DefaultSLAResolutionMinutes` tienen valores propios (15/30/60… min) que no coinciden con los de Seam. Para activo crítico con prioridad alta, la resolución es 960 min en molu y 1440 en Seam.
  - **Tabla de Seam** (minutos, por categoría y prioridad `critical`/`high`/`medium`/`low`; el valor por defecto es para una prioridad desconocida):

    | Categoría | Primera respuesta | Resolución |
    |---|---|---|
    | `critico` | 60 / 120 / 240 / 240 (def. 120) | 240 / 1440 / 1440 / 2880 (def. 1440) |
    | `normal` | 120 / 240 / 480 / 480 (def. 240) | 1440 / 2880 / 4320 / 10080 (def. 2880) |

  - **Hecho cuando**: para las 8 combinaciones los plazos coinciden con los de Seam, con un test que compara las tablas. Queda documentado que Seam permite sobrescribir los minutos por plantilla de incidencia (`slaMinutesFromTemplate`) y que el publicador usa solo los valores por defecto.

- [ ] **T10. Campos del documento como los escribe Seam.**
  - **Archivos**: `pkg/publisher/issues/types.go`, `service.go`, `registrar.go` (esquemas), tests
  - **Problema**: Seam guarda `channel`, `reported_by`, `assignee` y `updated_at`; el publicador no escribe `channel` ni `assignee`, y añade un campo `tenant` que Seam no usa.
  - **Hecho cuando**: `CreateIssue` escribe `channel = "api"` (valores válidos en Seam: `app`, `qr`, `api`), `reported_by` y `assignee` desde `reporter_id` (si no viene, se omiten), y no escribe `tenant` en el documento; `AdvanceIssue` actualiza `state` y `updated_at`; el documento resultante tiene las mismas claves que uno creado por Seam, salvo las que dependen de plantillas de Seam (`template_id`, `form_data`). Hay que decidir y documentar el valor de `channel` (`api` es el propuesto).

- [ ] **T11. Rechazo con motivo.**
  - **Archivos**: `pkg/publisher/issues/types.go`, `service.go`, `registrar.go`, tests
  - **Problema**: Seam exige un motivo para rechazar, lo envía como `payload.motivo` y lo guarda en `reject_reason`. `issues.AdvanceIssue` no tiene ese campo, así que rechaza sin motivo.
  - **Hecho cuando**: `AdvanceIssue` acepta `reason`; con `transition = reject` es obligatorio (si falta, `CONTRACT_VIOLATION` con `fields: ["reason"]`); se envía `{motivo, actor}` a la FSM y se guarda `reject_reason` en la incidencia; en las demás transiciones `reason` se rechaza o se ignora de forma documentada. También se envía `actor` en el payload de todas las transiciones, como hace Seam.

- [ ] **T12. Permisos de las transiciones (decisión previa) y casos agrupados.**
  - **Archivos**: por decidir (`pkg/publisher/issues/service.go`, `MEMORY.md` para el ADR)
  - **Problema**: Seam solo deja actuar al responsable de la incidencia (o a un administrador) y bloquea las que son hijas de un caso agrupado (`master_id > 0`; la transición del maestro se replica en los hijos). El publicador no aplica ninguna de las dos reglas, así que un agente puede avanzar una incidencia ajena o una hija.
  - **Hecho cuando**: se decide con el equipo (Gastón y Horacio) si el publicador debe aplicar estas reglas. Hace falta definir cómo llega la identidad y el rol del solicitante (`actor_id` hoy es un número opcional sin rol). Si se aplican: `INVALID_TRANSITION` o `NOT_OFFERED` con motivo claro (hijo de un caso, no es el responsable) y tests; si no, queda documentado como límite conocido en `MEMORY.md`. La replicación de transiciones a hijos se evalúa en la misma decisión.

- [ ] **T13. Pruebas contra xolu y Seam reales, y cierre documental.** *(El 26-nov-2026 se hizo una primera pasada manual con scripts, 16/26; ver arriba. Falta dejarla como prueba opcional en el repo y repetirla cuando T6–T12 y T14 estén hechas.)*
  - **Archivos**: `pkg/publisher/issues/real_xolu_test.go` (opcional con `MOLU_REAL_XOLU_URL`), `pkg/bridge_integration/issues_e2e_test.go`, `MEMORY.md`
  - **Hecho cuando**: existe una prueba opcional contra xolu real que cubre T6 a T11 (transición inválida, definición única con `merge`, criticidad `Alta`, SLA, campos, rechazo con motivo); la prueba E2E con xolu simulado refleja el documento y las reglas reales; `MEMORY.md` documenta el ADR de la alineación con el módulo de incidencias, y la fila de la Spec 010 vuelve a estar cerrada. Repetir la prueba de interoperabilidad con Seam 0.4.5 y la rama `main` de seam-CURE sin diferencias de documento.
