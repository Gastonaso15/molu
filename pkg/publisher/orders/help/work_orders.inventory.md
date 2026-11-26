summary: stock, depósitos, consumibles y cómo cargar y liquidar insumos de una orden
# Inventario y consumibles

## Stock general
- Las cantidades se dan en unidades reales con hasta 2 decimales. El stock entra, sale o se ajusta con `RegisterMovement` (`type` entry | exit | transfer | adjust, con `reason` y `user_id`). Lo consultas con `GetConsumable`, `GetWarehouse`, `ListConsumables` y `ListWarehouses`.
- **Stock bajo**: `ListLowStock` lista los consumibles con cualquier depósito en o por debajo de `stock_min`. Si ya no hay alertas responde `EMPTY` (no es un error). Para reponer, `RegisterMovement` tipo `entry` al depósito que indica la alerta.

## Consumibles de una orden
Una orden de una plantilla con consumibles tiene un **almacén móvil** (`mobile_warehouse_id` en `GetOrder`). Se usa así:
1. **Cargar** con `RecordOrderConsumable(order_id, consumable_id, user_id, intent, quantity, origin_warehouse_id)`: `confirm` carga un consumible planificado (los de la plantilla y los `extra_consumable_ids`), `add` uno que no lo está (solo si la plantilla permite extras), `more` otra carga del mismo, `skip` si no se usará. La carga es una transferencia desde el depósito de origen al almacén móvil. Exige la cabecera confirmada y la orden abierta.
2. **Liquidar** cuando la orden llegó al estado final: `SettleOrderConsumable(order_id, consumable_id, user_id, used, return_warehouse_id)`. Lo usado sale del almacén móvil y el sobrante vuelve al depósito indicado (obligatorio si `used` es menor que lo cargado). Cada consumible se liquida una vez.

**No uses `RegisterMovement` para cargar ni consumir en una orden**: el stock cambiaría sin que la orden lo sepa. Si el depósito no tiene el stock que hace falta, primero `RegisterMovement` tipo `entry` a ese depósito.
