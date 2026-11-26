---
name: work-orders-seam
description: Cómo operar Work Orders de Seam con las funciones work_orders.* del MCP molu (órdenes, plantillas, bloques, cuadrillas, tercerizados, depósitos, consumibles, stock). Úsalo cuando el usuario hable de órdenes de trabajo, tablero de órdenes, reclamar, ejecutar bloques, stock, consumibles, almacén móvil o cuadrillas.
---

# Work Orders en Seam vía molu

## Conceptos
- **Plantilla**: define estados (p. ej. Preparada → En traslado → Entregada), activos mínimos/máximos, ítems de cabecera, bloques y consumibles. `ListTemplates` / `GetTemplate`. Solo la última versión y habilitada abre órdenes (`template_not_latest`, `template_disabled`); `SetTemplateEnabled` la habilita o deshabilita.
- **Cabecera**: los ítems con `filled_at=upon_creating` se contestan al crear (`header_values`); los de `upon_executing` se contestan después, una sola vez, con `ConfirmExecutionHeader`.
- **Bloques**: unidades de trabajo con líneas (campos) y, a veces, un predecesor. Cada bloque tiene un `responsible_type` (user, crew o third_party) que sale de `GetTemplate`.
- **Orden**: `CreateOrder(template_id, asset_ids, ...)`. Asignación directa (`direct_assignment` + `responsible_type` + `responsible_id`) o **tablero** (`board_posting`, SIN responsible_id; alguien la reclama). Si la plantilla tiene bloques hay que pasar `block_responsibles {NombreBloque: id}`: el id de un usuario, cuadrilla o tercero según el tipo del bloque (casi siempre la misma cuadrilla de la orden).
- **Quién actúa**: casi todas las acciones piden `user_id`. Debe ser el responsable de la orden: el propio usuario, el responsable o un integrante de la cuadrilla (`ListCrews`/`GetCrew`), o el responsable del tercero. El responsable de un bloque también puede ejecutar ese bloque. El `user_id` no se verifica: si el usuario no dice quién es, usa el responsable de la cuadrilla y dilo en la respuesta.

## Ejecutar una orden (orden asignada o ya reclamada)
1. `ConfirmExecutionHeader(order_id, user_id, header_values)` con los ítems `upon_executing` obligatorios. Sin esto no arrancan los bloques ni la orden avanza.
2. Por cada bloque, en orden de predecesores: `ExecuteBlock(order_id, block, intent=start)` y luego `intent=complete` con `values {linea: valor}` (`save` guarda sin completar). Los valores se validan por tipo de línea (gps `"lat,lng"`, fecha `AAAA-MM-DD`, número, booleano).
3. `AdvanceOrder(order_id, input=complete, user_id)` pasa al siguiente estado. Entrar al último estado exige todos los bloques completados. Ya no se pide firma ni se mandan respuestas en el avance.
4. `GetOrder` muestra estado, bloques, respuestas por nombre (`block_answers`), `consumables` y el almacén móvil.

## Tablero
`ListOrders backlog=true` → `ClaimOrder(order_id, user_id)` con un usuario elegible (responsable de la cuadrilla o tercero que corresponde). Reclamar de nuevo con el mismo usuario no cambia nada; otro recibe `already_claimed`. `AssignOrder` reasigna mientras la orden no esté terminada.

## Inventario y consumibles
- Cantidades en unidades reales (máx. 2 decimales). El stock entra con `RegisterMovement` (`entry|exit|transfer|adjust`, con `reason` y `user_id`).
- Una orden de plantilla con consumibles tiene **almacén móvil** (`mobile_warehouse_id`). Se carga con `RecordOrderConsumable(order_id, consumable_id, user_id, intent, quantity, origin_warehouse_id)`: `confirm` un consumible planificado, `add` uno que no lo está (solo si la plantilla permite extras), `more` otra carga del mismo, `skip` si no se usará. Exige cabecera confirmada y orden abierta. **No uses `RegisterMovement` para cargar una orden.**
- Cuando la orden llega al estado final, `SettleOrderConsumable(order_id, consumable_id, user_id, used, return_warehouse_id)` cierra cada consumible cargado: lo usado sale, el sobrante vuelve al depósito indicado (obligatorio si `used` es menor que lo cargado). Cada consumible se liquida una vez.
- **Stock bajo**: `ListLowStock` lista consumibles con cualquier depósito en o bajo `stock_min`. Si ya no hay alertas responde `EMPTY` (no es un error).

## Flujos típicos
1. Orden con bloques: `GetTemplate` → `CreateOrder` (con `block_responsibles`) → `ConfirmExecutionHeader` → `ExecuteBlock` start/complete por bloque → `AdvanceOrder` hasta el último estado.
2. Orden con consumibles: lo anterior + `RegisterMovement entry` al depósito → `RecordOrderConsumable confirm` (tras confirmar la cabecera) → al terminar la orden, `SettleOrderConsumable`.
3. Orden del tablero: `ListOrders backlog=true` → `ClaimOrder` → `ConfirmExecutionHeader` → `AdvanceOrder`.
4. Reponer stock bajo: `ListLowStock` → `RegisterMovement entry` al depósito indicado en la alerta.

## Errores (no los reintentes a ciegas)
- `INVALID_TRANSITION` con `details.rule`: `not_responsible` (el usuario no es el responsable), `header_not_confirmed`, `predecessor_pending`, `blocks_incomplete`, `invalid_block_state`, `awaiting_claim`, `already_claimed`, `ineligible`, `not_finished` (liquidar antes de terminar), `final_state`, `insufficient_stock`, `already_recorded`, `template_disabled`, `template_not_latest`. Cambia la acción según la regla, no repitas.
- `CONTRACT_VIOLATION`: revisa `invalid_fields` (por ejemplo `block_responsibles.<bloque>`).
- `EMPTY`: consulta válida sin resultados. `NOT_FOUND`: el id no existe.

## Hábitos
Prefiere `work_orders.*` a las primitivas genéricas. Si un dato se puede deducir con una función `List*`/`Get*`, deducilo en vez de preguntar. Al terminar, resume ids, estados y stock resultantes.
