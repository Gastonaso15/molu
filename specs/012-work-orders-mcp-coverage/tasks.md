# Tareas: Spec 012 — Cobertura MCP de Work Orders

Una tarea por vez, con TDD (`go test -v -race ./...`). Al terminar cada una: marcar `[x]`, actualizar `MEMORY.md` y parar para confirmación.

## Fase 0 — Verificación de lo existente
- [x] **T1. Verificar contra xolu real lo ya publicado.** Levantar xolu con `scripts/levantar-xolu.sh`, cargar semilla 01 y `seeds/workorders/seed-wo-templates.py`; probar `ListTemplates`, `CreateOrder` (legacy y con plantilla), `GetOrder`, `AdvanceOrder` y un rechazo de FSM. Registrar discrepancias contra la rama `work_orders` (`a3bd15c`). RF-1, RF-8.
  - **Resultado (23-nov-2026)**: ver §7 de `spec.md`.

## Fase 1 — Lectura (bajo riesgo)
- [x] **T2. `ListOrders`.** Filtros: `state`, `assignment_type`, `responsible_type`, `responsible_id`, `template_id`, `asset_id`, `backlog`, `created_from/to`, `limit`. `EMPTY` si no hay resultados. RF-4, RF-6, RF-8.
  - **Hecho (23-nov-2026)**: `pkg/publisher/orders/list_orders.go` (+ `FindEntities` en `Substrate`/`HTTPSubstrate`, ruta `/api/v1/orders/list`, registro en el Hub). Pruebas: `list_orders_test.go` (unitarias, servidor, OQL) y `TestRealXolu_ListOrders` contra xolu real. Decisiones: filtros por OQL salvo `asset_id` (filtrado en memoria sobre las 500 últimas, `truncated` avisa); límite 50 por defecto y 200 máximo; `total` cuenta lo devuelto y `truncated` indica que hubo más; sin coincidencias responde `total: 0`, que el bridge convierte en `EMPTY`; prioridad y ubicación no existen en `work_orders` (pedido a WO).
- [x] **T3. `ListTemplates` ampliada y `GetTemplate`.** Bloques, consumibles, acciones, reglas, numeración. RF-6, RF-8.
  - **Hecho (23-nov-2026)**: `pkg/publisher/orders/template_detail.go`, tipos en `types.go`, ruta `/api/v1/orders/template`, registro en el Hub (6 funciones). `ListTemplates` devuelve la estructura completa sin resolver nombres; `GetTemplate` resuelve nombres de consumibles (`consumable`) y acciones (`wo_actions`), y un nombre ilegible queda vacío sin fallar. `NotFoundError` lleva ahora `Kind` para decir "work order template". Reglas (`rule_item`) se devuelven tal cual. `supported` no cambia hasta T8/T12. Pruebas: `template_detail_test.go` y `TestRealXolu_GetTemplate` contra la semilla real de WO.
- [x] **T4. `GetOrder` ampliado.** Bloques con estado, `block_values`, firma, almacén móvil con existencias, historial de FSM. RF-1, RF-8.
  - **Hecho (23-nov-2026)**: `pkg/publisher/orders/order_detail.go`; `GetOrder` devuelve `OrderDetail` (el `WorkOrder` de siempre más `created_at`, `updated_at`, `claimed_by_user_id`, `signed`, `block_values`, `blocks`, `mobile_warehouse`, `history`). Bloques desde `work_order_blocks` (orden por `block_index`, `predecessor_index` nulo si no hay). Existencias del almacén móvil con la regla de Seam: `consumable.stock_by_warehouse` (centésimas) manda; el saldo del documento del almacén solo cuenta para consumibles sin ese mapa; unidad por `unit.symbol`. Historial por `/api/v2/fsm/machine/{id}/history`, solo para órdenes legacy. La firma no viaja (es una imagen base64): solo `signed`. Una entidad inexistente se lee como vacía; otro fallo de lectura falla la llamada. Se añadieron `Substrate.MachineHistory` y `EntityQuery.OrderBy`. Se ajustó el xolu simulado del E2E de Criterio 1 (historial y consultas OQL por entidad). Pruebas: `order_detail_test.go` y `TestRealXolu_GetOrderDetail`.
- [x] **T5. `ListCrews/GetCrew`, `ListOutsourced/GetOutsourced`.** RF-4, RF-6, RF-8.
  - **Hecho (23-nov-2026)**: `pkg/publisher/orders/people.go`; rutas `/api/v1/orders/crews/{list,get}` y `/outsourced/{list,get}`; 10 funciones registradas. Los listados se ordenan por nombre y filtran en memoria sobre un máximo de 500 filas (catálogos pequeños, como en Seam; `truncated` avisa). `GetCrew`/`GetOutsourced` resuelven nombres desde la entidad `users` de xolu (como el detalle de cuadrilla de Seam) y de cada usuario solo se lee el nombre (nunca `password_hash` ni correo). La firma del tercerizado no viaja: `has_signature` + `signature_clarification`. Brechas para WO: no existe campo de **código** de tercerizado ni noción de **disponibilidad** de cuadrillas. Pruebas: `people_test.go` y `TestRealXolu_People`.
- [x] **T6. `ListWarehouses/GetWarehouse`, `ListConsumables/GetConsumable`, `ListLowStock`.** RF-1, RF-4, RF-6, RF-8.
  - **Hecho (23-nov-2026)**: `pkg/publisher/orders/inventory.go`; rutas `/api/v1/orders/{warehouses,consumables}/{list,get}` y `/stock/low`; 15 funciones registradas. Existencias con la regla de Seam (T4): `consumable.stock_by_warehouse` manda y el saldo del documento del almacén solo vale para consumibles sin ese mapa. `low_stock` y las alertas siguen la regla de Seam: una existencia por almacén `<= stock_min` (alcanzar el umbral ya cuenta, faltante 0); las alertas se ordenan por faltante y marcan los almacenes móviles (se incluyen, como en Seam; `warehouse_id` permite acotar). `ListWarehouses` incluye los móviles salvo `mobile=false`. Escaneo máximo de 500 filas por catálogo con `truncated`. Pruebas: `inventory_test.go` y `TestRealXolu_Inventory`. Las pruebas reales de T2–T6 ahora usan nombres únicos por ejecución y se pueden repetir sobre el mismo xolu.
  - **Pendiente de confirmar con WO**: si las alertas deben excluir almacenes móviles y consumibles deshabilitados (Seam no los excluye).

## Fase 2 — Ciclo de vida y asignación
- [ ] **T7. `ClaimOrder`** (backlog → reclamada, `_version`, elegibilidad por `responsible_user_id`). RF-3.
- [ ] **T8. `CreateOrder` con bloques.** FSM por bloque, `work_order_blocks`, predecesores sin ciclos, `Commit` único con tope de 25. RF-2.
- [ ] **T9. Avance de bloques en `AdvanceOrder`** (respeta predecesores; bloque `pending → in_progress → completed`).
- [ ] **T10. `AssignOrder`** (usuario/cuadrilla/tercerizado; regla de reasignación según respuesta de WO).

## Fase 3 — Stock y almacén móvil
- [ ] **T11. `RegisterMovement`** (`entry/exit/transfer/adjust`, unidades ×100, recibo idempotente, `Commit` atómico con `_version`). RF-2, RF-5.
- [ ] **T12. Almacén móvil en `CreateOrder`** (consumibles de plantilla, `mobile_warehouse_id`) y `ConsumeFromOrder` / `ReturnLeftovers`.

## Fase 4 — Escritura de configuración
- [ ] **T13. `CreateCrew/UpdateCrew`, `CreateOutsourced/UpdateOutsourced`.**
- [ ] **T14. `CreateWarehouse/UpdateWarehouse`, `CreateConsumable/UpdateConsumable`.**
- [ ] **T15. `CreateTemplate` y `PublishTemplateVersion`** (validación de estructura; `latest`/`version`).

## Fase 5 — Cierre
- [ ] **T16. Registro en el Hub de todas las funciones nuevas** (esquemas, `token_ref`, descripciones). RF-6.
- [ ] **T17. E2E por MCP** de un escenario completo: crear almacén y consumible, entrada de stock, crear WO con plantilla y bloques, reclamar, avanzar, consumir, devolver, cerrar.
- [ ] **T18. Documento de brechas final y lista de pedidos a WO** (§5), listo para Horacio y Slack.
