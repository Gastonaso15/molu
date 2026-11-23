# Tareas: Spec 012 — Cobertura MCP de Work Orders

Una tarea por vez, con TDD (`go test -v -race ./...`). Al terminar cada una: marcar `[x]`, actualizar `MEMORY.md` y parar para confirmación.

## Fase 0 — Verificación de lo existente
- [x] **T1. Verificar contra xolu real lo ya publicado.** Levantar xolu con `scripts/levantar-xolu.sh`, cargar semilla 01 y `seeds/workorders/seed-wo-templates.py`; probar `ListTemplates`, `CreateOrder` (legacy y con plantilla), `GetOrder`, `AdvanceOrder` y un rechazo de FSM. Registrar discrepancias contra la rama `work_orders` (`a3bd15c`). RF-1, RF-8.
  - **Resultado (23-nov-2026)**: ver §7 de `spec.md`.

## Fase 1 — Lectura (bajo riesgo)
- [x] **T2. `ListOrders`.** Filtros: `state`, `assignment_type`, `responsible_type`, `responsible_id`, `template_id`, `asset_id`, `backlog`, `created_from/to`, `limit`. `EMPTY` si no hay resultados. RF-4, RF-6, RF-8.
  - **Hecho (23-nov-2026)**: `pkg/publisher/orders/list_orders.go` (+ `FindEntities` en `Substrate`/`HTTPSubstrate`, ruta `/api/v1/orders/list`, registro en el Hub). Pruebas: `list_orders_test.go` (unitarias, servidor, OQL) y `TestRealXolu_ListOrders` contra xolu real. Decisiones: filtros por OQL salvo `asset_id` (filtrado en memoria sobre las 500 últimas, `truncated` avisa); límite 50 por defecto y 200 máximo; `total` cuenta lo devuelto y `truncated` indica que hubo más; sin coincidencias responde `total: 0`, que el bridge convierte en `EMPTY`; prioridad y ubicación no existen en `work_orders` (pedido a WO).
- [ ] **T3. `ListTemplates` ampliada y `GetTemplate`.** Bloques, consumibles, acciones, `latest`. RF-6.
- [ ] **T4. `GetOrder` ampliado.** Bloques con estado, `block_values`, firma, almacén móvil con existencias, historial de FSM.
- [ ] **T5. `ListCrews/GetCrew`, `ListOutsourced/GetOutsourced`.**
- [ ] **T6. `ListWarehouses/GetWarehouse`, `ListConsumables/GetConsumable`, `ListLowStock`.**

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
