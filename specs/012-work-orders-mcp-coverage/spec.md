# Spec 012 — Cobertura MCP del módulo Work Orders

**Estado**: Borrador para revisión (22-nov-2026); actualizado el 26-nov-2026 al commit `25e7fa7` (ver §8)
**Origen**: correo de Granero (propuesta de funciones WO para MCP) y correo de Horacio (el equipo IA verifica, inventaría brechas y solo entonces pide ayuda a WO).
**Referencia de WO**: únicamente la rama `work_orders` de `Gastonaso15/seam-CURE`, commit `25e7fa7` (26-nov-2026; hasta el 25-nov era `a3bd15c`, y §1–§7 se escribieron contra ese commit: donde difieren, manda §8). Las ramas de Seam son versiones paralelas que se unirán más adelante; las demás copias locales (por ejemplo `seam-v0.4.5-checkpoint`) no son referencia para este spec.
**Código de referencia leído**: `internal/ui/{workorders,workorders_templates,workorders_blocks_helper,workorders_helper,crew,outsourced,warehouse,consumable,movement,wo_action}.go`.

## 1. Objetivo

Que un agente de IA pueda hacer, vía MCP, todo lo que lista el correo de Granero, con semántica de alto nivel (funciones `work_orders.*` del publicador) o, cuando basta, con los primitivos genéricos del Hub. Se entrega:

1. Un inventario verificado de qué funciona hoy y qué falta (§3).
2. Las funciones que faltan, implementadas en `pkg/publisher/orders`.
3. La lista de pedidos concretos al equipo WO (§5).

## 2. Hechos del módulo real que condicionan el diseño

| Tema | Hecho en Seam (rama `work_orders`) |
|---|---|
| Entidades xolu | `work_orders`, `work_order_templates`, `work_order_blocks`, `crews`, `outsourced`, `warehouse`, `consumable`, `consumable_types`, `movement`, `movement_request`, `wo_actions`. Nota: la entidad de tercerizados se llama `outsourced` (no `third_party`) y la de almacén es `warehouse` (singular). |
| Dos modos de WO | Con plantilla (`workflow_mode=template`, estados de la plantilla, sin FSM de xolu, avance por PATCH con `_version`) y legacy (FSM `seam_work_order_lifecycle`). |
| Bloques | Cada bloque de la plantilla crea una entidad `work_order_blocks` y una máquina FSM propia (`seam_work_order_block_lifecycle_v1`: `pending → in_progress → completed`), con predecesores (índices) y sin ciclos. Todo se crea en un único `Commit` (máx. 25 appends). |
| Almacén móvil | Si `ConsumablesEnabled`, la creación hace append de un `warehouse` `type=mobile` con `work_order_id` y la WO guarda `mobile_warehouse_id` (mismo id que la orden). |
| Backlog / reclamar | `assignment_type=board_posting` y `responsible_id=0` es el backlog. Reclamar = PATCH con `_version` que fija `responsible_id`, `assigned_to`, `claimed_by_user_id`. Quien reclama debe ser el `responsible_user_id` de la cuadrilla/tercerizado, o él mismo si es `user`. |
| Stock | El stock vive en `consumable.stock_by_warehouse` (mapa almacén → unidades enteras ×100). Un movimiento (`entry`, `exit`, `transfer`, `adjust`) es un `Commit` atómico: actualiza el consumible con `_version` y agrega `movement` y `movement_request` (recibo idempotente, `request_id` de 32 hex, id del recibo derivado del propio `request_id`). Seam serializa con un mutex en proceso. |
| Usuarios | Viven en el almacén de cuentas de Seam, no en xolu (ya documentado en ADR-012). `responsible_user_id` en `crews`, `outsourced` y `warehouse` es un id de usuario. |
| Plantillas | Versionadas por campos `version` y `latest` en `work_order_templates`. No hay campo de activación ni de archivado: ese pedido del correo no existe en el módulo real (ver §5). |
| Acciones/reglas | `wo_actions` (catálogo con `enabled` y `parameters`) enlazadas desde la plantilla (`actions` con `trigger_point`, `block_reference`, `mapping_values`). La ejecución de acciones no está en el módulo; solo la configuración. |

## 3. Inventario de brechas (una fila por función del correo)

Leyenda de "Primitivos hoy": ✅ resuelto con `list/get/find/create/update` sin lógica de dominio; ⚠️ técnicamente posible pero el agente tendría que conocer reglas de Seam y puede dejar datos inconsistentes; ❌ no viable.
"Publicador hoy" se refiere a `work_orders.{CreateOrder,GetOrder,AdvanceOrder,ListTemplates}`.

### Plantillas y configuración
| # | Función del correo | Primitivos hoy | Publicador hoy | Decisión |
|---|---|---|---|---|
| 1 | Listar / consultar plantillas (versión activa, bloques, campos, reglas/acciones) | ✅ lectura | Parcial: `ListTemplates` no devuelve bloques, consumibles ni acciones | **Ampliar** `ListTemplates` y **agregar** `GetTemplate` |
| 2 | Crear y editar plantillas | ⚠️ `create` con la estructura anidada exacta | No | **Nueva** `CreateTemplate` (valida estructura) — ver pregunta de alcance |
| 3 | Publicar / versionar plantilla | ⚠️ hay que dejar `latest=false` en la anterior y `version+1` | No | **Nueva** `PublishTemplateVersion` (o incluida en edición) |
| 4 | Activar / desactivar / archivar plantilla | ✅ desde `25e7fa7` (campo `enabled`; no hay archivado aparte) | **Nueva** `SetTemplateEnabled` (T19, hecha) | Hecho; ya no es un pedido a WO (§8) |

### Ciclo de vida y ejecución
| # | Función | Primitivos hoy | Publicador hoy | Decisión |
|---|---|---|---|---|
| 5 | Crear WO desde plantilla | ⚠️ | Sí, **sin** bloques ni almacén móvil | **Ampliar** `CreateOrder`: bloques (FSM por bloque), consumibles y almacén móvil, en un solo `Commit` |
| 6 | Consultar WO / expediente | ✅ `get` | `GetOrder` básico | **Ampliar** `GetOrder`: bloques con estado, consumos, almacén móvil, `block_values`, firma, historial de FSM |
| 7 | Listar / filtrar WO | ✅ `find` con OQL | No hay `ListOrders` | **Nueva** `ListOrders` (estado, prioridad*, activo, ubicación*, asignatario, fechas, backlog) |
| 8 | Cambiar estado (FSM) | ⚠️ | `AdvanceOrder` (legacy y plantilla sin bloques) | **Ampliar**: avance de bloques; **Nueva** `ClaimOrder`; pausar/cancelar/aprobar solo si WO los define (ver §5) |

\* Prioridad y ubicación: no aparecen como campos de `work_orders` en el módulo real; ver §5.

### Asignación y entidades
| # | Función | Primitivos hoy | Publicador hoy | Decisión |
|---|---|---|---|---|
| 9 | Asignar / reasignar WO | ⚠️ PATCH con `_version` | Solo en la creación | **Nueva** `AssignOrder` (usuario, cuadrilla, tercerizado; reasignar solo si WO lo permite) |
| 10 | Gestionar cuadrillas | ✅ CRUD plano (`crews`: `name`, `description`, `responsible_user_id`, `members`) | No | **Nuevas** `ListCrews`, `GetCrew`; crear/editar con `CreateCrew`/`UpdateCrew` |
| 11 | Gestionar tercerizados | ✅ CRUD plano (`outsourced`) | No | **Nuevas** `ListOutsourced`, `GetOutsourced`, `CreateOutsourced`, `UpdateOutsourced` |

### Almacenes, consumibles y almacén móvil
| # | Función | Primitivos hoy | Publicador hoy | Decisión |
|---|---|---|---|---|
| 12 | Crear / editar / listar almacenes | ✅ CRUD plano | No | **Nuevas** `ListWarehouses`, `GetWarehouse` (con existencias), `CreateWarehouse`, `UpdateWarehouse` |
| 13 | Crear / editar / listar consumibles | ✅ CRUD plano (stock mínimo en `stock_min`) | No | **Nuevas** `ListConsumables`, `GetConsumable`, `CreateConsumable`, `UpdateConsumable` |
| 14 | Movimientos de stock | ❌ un `create` en `consumable` rompe el stock (unidades ×100, recibo idempotente, `Commit` atómico con `_version`) | No | **Nueva** `RegisterMovement` que replica el `Commit` de Seam |
| 15 | Almacén móvil de WO (abrir, consumir, devolver sobrantes) | ❌ | No | **Nuevas** `ConsumeFromOrder` y `ReturnLeftovers` sobre `RegisterMovement`; la apertura la hace `CreateOrder` |
| 16 | Alertas de stock mínimo | ⚠️ cruce de `stock_min` con `stock_by_warehouse` | No | **Nueva** `ListLowStock` |

## 4. Requisitos (EARS)

- **RF-1** Cuando un agente llame a una función `work_orders.*`, el publicador **deberá** reproducir las mismas reglas que la UI de Seam (campos, estados, `_version`, unidades ×100) para que lo escrito sea legible en Seam sin reparación.
- **RF-2** Cuando una escritura entre varias entidades deba ser atómica (orden + bloques + almacén móvil; consumible + movimiento + recibo), el publicador **deberá** usar `Commit` de xolu y no varias escrituras sueltas.
- **RF-3** Si xolu rechaza una transición o hay un conflicto de `_version`, el publicador **deberá** devolver `INVALID_TRANSITION` o `stale_state` conservando el diagnóstico de xolu verbatim (ADR-010, ADR-012).
- **RF-4** Cuando una entidad o filtro no tenga resultados, la función **deberá** devolver `EMPTY` y no una lista rellena (§2.4).
- **RF-5** El registro de un movimiento **deberá** ser idempotente por `request_id`: repetir la misma petición no duplica el movimiento; el mismo `request_id` con otro contenido se rechaza.
- **RF-6** Cada función nueva **deberá** registrarse en el Hub con esquema JSON de entrada, `token_ref` y descripción semántica (para que el agente no tenga que inferir el dominio).
- **RF-7** El código del publicador **no deberá** contener la palabra "Seam" (ADR-004, `grep -ri seam cmd/ pkg/` = 0).
- **RF-8** Cada función nueva **deberá** tener prueba unitaria contra xolu simulado y, donde sea posible, prueba contra xolu real con la semilla de WO (`seeds/workorders/*.py`).

## 5. Pedidos para el equipo WO (cuando se confirmen)

1. ~~Campo y regla de activar/desactivar/archivar plantilla (fila 4).~~ **Resuelto en `25e7fa7`** (campo `enabled`; solo la versión `latest` se puede cambiar; no hay archivado aparte). Ver §8.
2. **Prioridad** y **ubicación** de la WO (fila 7): confirmar si existen o si se difieren.
3. **Pausar, cancelar y aprobar** (fila 8): el ciclo de plantilla solo avanza con `complete`. Confirmar si son estados de plantilla o transiciones aún por construir.
4. **Reasignar** (fila 9): hoy solo se asigna al crear o al reclamar. Confirmar la regla.
5. **Consumo efectivo y devolución de sobrantes en la WO** (fila 15): el almacén móvil se crea vacío; no vimos en el módulo el flujo de consumo ni de cierre. Pedir las reglas.
6. **Usuarios**: cómo resolver un usuario por id/email desde fuera de la UI (viven en cuentas de Seam, no en xolu).
7. **Código de tercerizado y disponibilidad de cuadrillas** (filas 10 y 11): el módulo no tiene campo de código para `outsourced` ni noción de disponibilidad de `crews`; confirmar si se difieren.
8. **Alertas de stock mínimo** (fila 16): Seam marca el consumible si cualquier almacén (también los móviles de órdenes y aunque el consumible esté deshabilitado) está en `stock_min` o menos. Confirmar si es lo deseado o si deben excluirse.
9. **Ciclo de vida de los bloques** (fila 8): al crear la orden Seam deja una máquina y un registro por bloque en `pending`, pero la rama `work_orders` no tiene ninguna ruta ni lógica que los inicie o complete: las respuestas van en `block_values` de la orden. Confirmar si la ejecución de bloques (predecesores, `start`/`complete`) está prevista y cuándo.
10. **Identidad del usuario que actúa**: el reclamo (y las futuras asignaciones/movimientos) necesitan saber quién actúa. Hoy viaja como `user_id` en la llamada, sin verificar. Definir con Horacio si el Hub propagará la identidad del usuario autenticado.
11. Notas por función (como pide Horacio) cuando suban la versión estable.

## 6. Fuera de alcance

Ejecución automática de acciones/reglas de plantilla (el módulo solo guarda su configuración), notificaciones, evidencias/adjuntos binarios, y cualquier cambio en Seam o xolu.

## 7. Resultados de T1 — verificación contra xolu real (23-nov-2026)

Entorno: xolu 0.31.0 limpio (puerto 9595, `levantar-xolu-macos.sh --no-seed`) más `seed-wo-actions.py` y `seed-wo-templates.py` de la rama `work_orders`. Sin usuarios, sin almacenes y sin existencias (la semilla no los crea).

**Funciona**
- `go test -race` del publicador y del Hub: verde. `TestRealXolu_Lifecycle` y `TestRealXolu_TemplateOrder` pasan contra el xolu real: crear, leer, rechazo de FSM (`XOLU-FSM003`, `XOLU-FSM004`) con diagnóstico verbatim, avance de plantilla, `stale_state` y conflicto de `_version` (409 `XOLU-ST005`).
- `ListTemplates` lee las 3 plantillas de la semilla y los campos de cabecera (`upon_creating`/`upon_executing`).
- Primitivos de molu por MCP (stdio) sobre entidades de WO: `list` de `crews` y `work_orders`, `get` de `consumable`, `create` de `crews`, `query` OQL. Los primitivos son 13 y el argumento es `entity_type` (no `entity`).

**Brechas confirmadas**
- Las **3 plantillas de la semilla tienen consumibles y 2 además bloques**: `CreateOrder` las rechaza (`supported=false`). Hoy ninguna plantilla de demostración de WO se puede instanciar desde MCP. Es la brecha principal (filas 5, 12, 15).
- `ListTemplates` ignora `blocks`, `consumables`, `actions`, `rule_item`, `consumables_allowed` y `add_any_consumables_allowed`, que sí están en el documento guardado.
- La orden legacy creada por el publicador no guarda `block_values`, `assignment_type`, `responsible_type` ni `responsible_id`, que Seam sí escribe. Seam los lee con valores por defecto, así que se ve bien, pero el listado de backlog filtra por esos campos. Conviene alinearlos en T8.

**Observaciones sobre los primitivos**
- `query` sobre `warehouse` (aún sin ninguna fila) devuelve el error `XOLU-QL004` y no `EMPTY`. El publicador ya trata "entidad no creada" como lista vacía; los primitivos no.
- `walk` sobre una máquina terminal devuelve `XOLU-FSM005` crudo, no `INVALID_TRANSITION`; el publicador sí lo tipa.
- `create` sobre `crews` acepta cualquier contenido sin validar (por ejemplo un `responsible_user_id` inexistente): confirma que la escritura por primitivos es insegura para el agente.

**Efectos colaterales**: el xolu de prueba quedó con datos de las pruebas (órdenes, plantillas 4 y 5, cuadrilla "Cuadrilla MCP") y sigue corriendo en el puerto 9595 (`./scripts/levantar-xolu-macos.sh stop --port 9595`). Se creó `molu/.token.dev` y se recompilaron los binarios en `bin/`.

## 8. Actualización al commit `25e7fa7` (26-nov-2026)

El 26-nov-2026 la rama `work_orders` recibió el commit `25e7fa7` ("Enable/Disable templates added… Progress on Execution of WO…", 455 archivos). Se leyó `a3bd15c..25e7fa7` en lo que toca a Work Orders; el resto (seeds en formato `.seed`, incidencias, CSS) no se revisó.

### 8.1 Resuelto: activar/desactivar plantilla (fila 4, pedido 1)
- `work_order_templates.enabled` (booleano; un documento sin el campo se lee como habilitado). Se cambia con PATCH `{"enabled": bool}` (`WorkOrderTemplateSetEnabled`, `internal/ui/workorders_templates.go`).
- Solo se puede cambiar la versión `latest` (409 si no). Una versión nueva nace con `enabled: true`.
- Crear una WO desde una plantilla deshabilitada o no `latest` se rechaza con 409 (`internal/ui/workorders.go`, `workOrderTemplateForCreate`).
- **Hecho en molu (T19)**: `work_orders.SetTemplateEnabled` (`template_enabled.go`); `Template.enabled` en `ListTemplates`/`GetTemplate`; `CreateOrder` rechaza una plantilla deshabilitada con `INVALID_TRANSITION` (`details.rule=template_disabled`); `CreateTemplate`/`PublishTemplateVersion` guardan `enabled: true`. 31 funciones registradas. Pruebas unitarias y HTTP; **no** se probó contra xolu real.
- **Diferencia consciente**: Seam hace el PATCH sin `_version`; molu usa `_version` (un cambio concurrente da `stale_state`). Repetir el estado actual no escribe (`changed: false`).
- **Brecha que queda**: `CreateOrder` de molu no comprueba que la plantilla sea `latest` (Seam sí). Es una divergencia previa, no resuelta aquí.

### 8.2 Nuevas divergencias de ejecución (no implementadas; sin pruebas contra xolu real)
Seam dejó de ser "solo configuración" en la ejecución. Esto invalida supuestos de §2, §5 (pedidos 5 y 9) y de T9/T12:

1. **Ejecución de bloques** (`workorders_execution.go`): ya existen rutas que (a) confirman los campos de cabecera "al ejecutar" (`execution_header_completed_at`; antes de eso la cabecera no está lista), (b) inician (`pending → in_progress`), guardan y completan un bloque, con su FSM por bloque en un `Commit`, respetando predecesores y que quien actúa sea el responsable del bloque. Las respuestas ya no van en `block_values` de la orden sino en `values` de cada `work_order_blocks`. `AdvanceOrder` de molu (T9) escribe en `block_values`, que Seam ya no usa para esto.
2. **Avance de la orden**: Seam exige cabecera confirmada y, para llegar al último estado, **todos los bloques `completed`** (`canAdvance`). `AdvanceOrder` de molu no lo exige: puede cerrar órdenes que Seam no dejaría cerrar.
3. **Consumo de insumos** (`workorders_execution.go`, `movement.go`): el modelo cambió. Registrar un consumo es una **transferencia desde un almacén de origen hacia el almacén móvil**, con un registro `work_order_consumptions` (`used`/`skipped`, `request_id` derivado de orden+consumible) y el mismo `Commit` que el movimiento. Al terminar se **liquida** (`intent=settle`): la cantidad usada sale (`exit`) del almacén móvil y el sobrante se transfiere a un almacén de devolución, todo en un `Commit` y con un registro `settled`. `ConsumeFromOrder` (hace `exit` desde el móvil) y `ReturnLeftovers` (T12) no coinciden con este flujo y no escriben `work_order_consumptions`; órdenes tocadas por molu quedarían sin registro de consumo en la UI de Seam.
4. **Movimientos**: el asiento `movement` ahora sale de `movementLedgerEntry` con los mismos campos; la lógica de `RegisterMovement` (T11) no cambió en sustancia. Pendiente comprobar con un caso real que molu y Seam escriben el mismo asiento.
5. Nuevas entidades: `work_order_consumptions`; la orden guarda `execution_header_completed_at/_by_user_id`, `extra_consumable_ids` (nombre exacto por confirmar en el código) y los bloques `values`, `started_at`, `completed_at`.

### 8.3 Efecto sobre la cobertura
- Fila 4: de "no viable" a **hecha** (T19). Las 16 funciones del correo tienen ya herramienta.
- Fila 8 (cambiar estado): realineada en T20 (§8.4): confirmar cabecera, ejecutar bloques y avanzar con la máquina propia de la orden. Sigue sin existir pausar/cancelar/aprobar.
- Fila 15 (almacén móvil): sigue **pendiente de realinear** (T21) hasta que molu replique el flujo nuevo de consumo y liquidación. Lo ya construido sigue funcionando contra el contrato de `a3bd15c`, pero puede dejar datos que Seam no interpreta igual.
- Pedidos a WO que cambian: 1 resuelto; 5 y 9 respondidos en gran parte por el código nuevo (falta confirmar con WO que ese es el diseño definitivo); el resto sigue abierto.

### 8.4 T20 hecha: ejecución de bloques y avance (26-nov-2026)
Contra la rama `work_orders` en `25e7fa7`. Funciones del publicador: 33 (`work_orders.ConfirmExecutionHeader` y `work_orders.ExecuteBlock` nuevas; `CreateOrder`, `AdvanceOrder` y `GetOrder` cambian de contrato).

**Qué cambió respecto a §2 y a T8/T9**
- **Máquina propia por orden**: toda orden con plantilla crea una máquina de la definición `seam_work_order_template_<plantilla>_v<versión>` (estados de la plantilla en orden, una transición `advance` entre consecutivos, el último terminal) y guarda su `machine_id`. Antes las órdenes con plantilla no tenían máquina.
- **Responsable por bloque**: `CreateOrder` exige `block_responsibles` (por nombre de bloque) cuando la plantilla tiene bloques. El tipo (user, crew, third_party) sale de la plantilla; se valida que exista (y que el usuario esté activo). Cada registro de bloque guarda `responsible_type` y `responsible_id`.
- **`ConfirmExecutionHeader`** (`user_id`, `header_values`): confirma una sola vez los ítems "al ejecutar" y los congela (`execution_header_completed_at/_by_user_id`). Solo el responsable de la orden; la orden debe estar reclamada y abierta.
- **`ExecuteBlock`** (`block`, `intent` start|save|complete, `user_id`, `values` por nombre de línea): exige cabecera confirmada y predecesor completado; actúa el responsable de la orden o el del bloque. `start` y `complete` guardan el bloque y mueven su máquina en un único `Commit` con `fsm_walk`; `save` solo guarda. Las respuestas van en `values` del bloque con las claves `template_block_<i>_line_<j>`. `GetOrder` las vuelve a mostrar por nombre en `block_answers`.
- **`AdvanceOrder`** en órdenes con máquina: exige `user_id` (responsable de la orden), cabecera confirmada, y todos los bloques completados para entrar al último estado; un `Commit` actualiza la orden y dispara `advance` en su máquina. Ya no acepta `header_values`, `block_values` ni firma para esas órdenes.
- Las órdenes **sin máquina** (abiertas antes de T20) siguen avanzando con la ruta antigua (un solo PATCH). **Seam ya no puede ejecutarlas en su pantalla** ("template work order has no FSM machine").
- El almacén móvil se nombra `<título> - dd-mm-aaaa` (hora de Montevideo), como Seam.

**Hallazgos al probar contra xolu real** (127.0.0.1:9595)
- xolu rechaza un `Commit` sin `append` ni `fsm_walk` (`XOLU-CM003`). Una orden sin bloques ni almacén móvil no tiene nada que anexar, así que se crea con un create simple (`creation_token` para resolver un resultado incierto). El doble de pruebas no lo detectaba. Por lectura del código, la rama de Seam también envía un commit así cuando hay máquina de plantilla; no se comprobó en vivo.
- Los 18 `TestRealXolu_*` pasan, incluidos `ExecuteBlocks` (historial de las máquinas de cada bloque y de la orden) y `SetTemplateEnabled`.

**Limitaciones conocidas**
- La identidad del que actúa (`user_id`) la informa el cliente, sin verificar (igual que `ClaimOrder`).
- La firma del técnico ya no se pide en el avance de las órdenes con máquina (Seam no la recoge en esta ruta); `signed` en `GetOrder` solo vale para órdenes antiguas. Falta confirmar con WO dónde queda la firma.
- No se replicó la comprobación de Seam de que el estado de la máquina coincida con el de la orden al cargar (el sustrato no expone leer una máquina); sí se comprueba el estado que devuelve el `fsm_walk`.
- `ConsumeFromOrder` y `ReturnLeftovers` no exigen cabecera confirmada ni responsable (Seam sí): T21.
- `ListTemplates`/`GetTemplate` no muestran aún `responsible_type` por bloque como dato destacado para el agente (sí va en `blocks`).
