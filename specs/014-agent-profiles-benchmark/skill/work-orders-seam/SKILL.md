---
name: work-orders-seam
description: Cómo operar Work Orders de Seam con las funciones work_orders.* del MCP molu (órdenes, plantillas, cuadrillas, tercerizados, depósitos, consumibles, stock). Úsalo cuando el usuario hable de órdenes de trabajo, tablero de órdenes, reclamar, stock, consumibles, almacén móvil o cuadrillas.
---

# Work Orders en Seam vía molu

## Conceptos
- **Plantilla**: define bloques, estados (p. ej. Preparada → En traslado → Entregada), activos mínimos/máximos y consumibles. `ListTemplates` / `GetTemplate`.
- **Orden**: se crea con `CreateOrder(template_id, asset_ids, ...)`. Asignación directa (`direct_assignment` + `responsible_type` user|crew|third_party + `responsible_id`) o **tablero** (`board_posting`, SIN responsible_id; alguien la reclama después).
- **Avanzar**: `AdvanceOrder(order_id, input="complete")` pasa al siguiente estado. Respuestas de bloques: `block_values {Bloque:{linea:valor}}`; campos de cabecera que se llenan al ejecutar: `header_values` (ver `header_items` con `filled_at=upon_executing` en `GetTemplate`). El paso que llega al último estado exige `payload.technician_signature`.
- **Reclamar**: `ClaimOrder(order_id, user_id)`. Solo puede reclamar el usuario responsable de la cuadrilla (o tercero) que corresponde; ver `ListCrews` / `ListOutsourced`. Reclamar dos veces con el mismo usuario no cambia nada; otro usuario recibe `already_claimed`.
- **Reasignar**: `AssignOrder` mientras la orden no esté terminada.
- **Inventario**: cantidades en unidades reales. El stock inicial solo entra con `RegisterMovement` (`entry|exit|transfer|adjust`, con `reason` y `user_id`). Una orden con consumibles tiene **almacén móvil** (`mobile_warehouse_id` en `GetOrder`): se carga con `RegisterMovement` tipo `transfer` desde un depósito, se gasta con `ConsumeFromOrder` y lo que sobra vuelve con `ReturnLeftovers`.
- **Stock bajo**: `ListLowStock` lista consumibles con cualquier depósito en o bajo `stock_min`. Si ya no hay alertas responde `EMPTY` (no es un error).
- El `user_id` que actúa no se verifica: si el usuario no dice quién es, usa el responsable de la cuadrilla (`ListCrews`) y dilo en la respuesta.

## Flujos típicos
1. Orden con consumibles: `CreateOrder` → `RegisterMovement entry` al depósito → `RegisterMovement transfer` al almacén móvil → `ConsumeFromOrder` → `ReturnLeftovers`.
2. Orden del tablero: `ListOrders backlog=true` → `ClaimOrder` → `AdvanceOrder`.
3. Reponer stock bajo: `ListLowStock` → `RegisterMovement entry` al depósito indicado en la alerta.

## Errores (no los reintentes a ciegas)
- `INVALID_TRANSITION` con `details.rule`: `final_state` (orden terminada), `already_claimed`, `not_claimable`, `ineligible`, `awaiting_claim`. Cambia la acción, no repitas.
- `CONTRACT_VIOLATION`: revisa `invalid_fields`.
- `EMPTY`: consulta válida sin resultados. `NOT_FOUND`: el id no existe.

## Hábitos
Prefiere `work_orders.*` a las primitivas genéricas. Si un dato se puede deducir con una función `List*`/`Get*`, deducilo en vez de preguntar. Al terminar, resume ids, estados y stock resultantes.
