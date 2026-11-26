summary: conceptos de órdenes de trabajo: plantillas, órdenes, asignación, quién actúa y tablero
# Órdenes de trabajo (`work_orders.*`)

## Conceptos
- **Plantilla**: define los estados de la orden (por ejemplo Preparada → En traslado → Entregada), los activos mínimos y máximos, los ítems de cabecera, los bloques y los consumibles. `ListTemplates` y `GetTemplate`. Solo la última versión y habilitada abre órdenes (`template_not_latest`, `template_disabled`); `SetTemplateEnabled` la habilita o deshabilita.
- **Cabecera**: los ítems con `filled_at=upon_creating` se contestan al crear (`header_values`); los de `upon_executing` se contestan después con `ConfirmExecutionHeader`.
- **Bloques**: unidades de trabajo con líneas (campos) y a veces un predecesor. Cada bloque tiene un `responsible_type` (user, crew o third_party) que sale de `GetTemplate`.
- **Orden**: `CreateOrder(template_id, asset_ids, ...)`. Asignación directa (`assignment_type=direct_assignment` + `responsible_type` + `responsible_id`) o **tablero** (`board_posting`, sin `responsible_id`: alguien la reclama después). Si la plantilla tiene bloques, pasa `block_responsibles {NombreBloque: id}`: el id de un usuario, cuadrilla o tercero según el tipo del bloque (casi siempre la misma cuadrilla de la orden). Para insumos que la plantilla permite añadir, `extra_consumable_ids`.

## Quién actúa
Casi todas las acciones piden `user_id`. Debe ser el responsable de la orden: el propio usuario, el responsable o un integrante de la cuadrilla (`ListCrews`, `GetCrew`) o el responsable del tercero. El responsable de un bloque también puede ejecutar ese bloque. El `user_id` no se verifica: si el usuario no dice quién es, usa el responsable de la cuadrilla y dilo en la respuesta.

## Tablero y asignación
`ListOrders backlog=true` lista las órdenes sin dueño. `ClaimOrder(order_id, user_id)` la reclama con un usuario elegible (responsable de la cuadrilla o tercero que corresponde); reclamar de nuevo con el mismo usuario no cambia nada y otro usuario recibe `already_claimed`. `AssignOrder` reasigna mientras la orden no esté terminada.

## Consultas
`GetOrder` muestra estado, bloques, respuestas por nombre (`block_answers`), consumibles y almacén móvil. `ListOrders` filtra por estado, plantilla, responsable, activo o fechas. Prioridad y ubicación no son campos de la orden.

Para ejecutar una orden: tema `work_orders.execution`. Para consumibles y stock: `work_orders.inventory`.
