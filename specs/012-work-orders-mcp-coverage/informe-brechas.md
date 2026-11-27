# Work Orders por MCP: cobertura final y pedidos al equipo de WO

Fecha: 27-nov-2026. Referencia de WO: rama `work_orders` de `seam-CURE`, commit `25e7fa7` (26-nov-2026). Es el cierre de la spec 012 (T18); el detalle técnico está en `spec.md` §3 y §8.

## 1. Respuesta corta

**Las 16 funciones del correo de Granero tienen función MCP** (33 funciones `work_orders.*` en total). **9 están completas y 7 están cubiertas con un faltante** que no depende de molu: Seam todavía no tiene ese campo o esa regla (los pedidos están en §3). Antes del commit del 26-nov eran 15 de 16; la que faltaba, activar/desactivar plantilla, ya la implementó WO.

## 2. Las 16 funciones

| # | Función del correo | Funciones MCP | Estado |
|---|---|---|---|
| 1 | Listar / consultar plantillas | `ListTemplates`, `GetTemplate` (bloques, consumibles, acciones, reglas, `enabled`) | Completa |
| 2 | Crear y editar plantillas | `CreateTemplate`, `PublishTemplateVersion` | Con faltante: una plantilla no se edita en sitio, se publica una versión nueva (así lo hace Seam) |
| 3 | Publicar / versionar | `PublishTemplateVersion` | Completa |
| 4 | Activar / desactivar / archivar | `SetTemplateEnabled` | Completa (Seam tiene `enabled`; no hay archivado aparte) |
| 5 | Crear WO | `CreateOrder` (responsable por bloque, insumos extra, cabecera, activos) | Completa |
| 6 | Consultar / expediente | `GetOrder`, `ListOrders` | Con faltante: no existen evidencias ni novedades en Seam; el historial de FSM solo para órdenes sin plantilla; la firma ya no se pide |
| 7 | Listar / filtrar | `ListOrders` (estado, plantilla, responsable, activo, fechas, tablero) | Con faltante: prioridad y ubicación no son campos de la orden |
| 8 | Cambiar estado | `ConfirmExecutionHeader`, `ExecuteBlock`, `AdvanceOrder`, `ClaimOrder` | Con faltante: pausar, cancelar y aprobar no existen en Seam |
| 9 | Asignar / reasignar | `AssignOrder`, `ClaimOrder` | Completa |
| 10 | Cuadrillas | `ListCrews`, `GetCrew`, `CreateCrew`, `UpdateCrew` | Con faltante: no hay disponibilidad |
| 11 | Tercerizados | `ListOutsourced`, `GetOutsourced`, `CreateOutsourced`, `UpdateOutsourced` | Con faltante: no hay campo de código |
| 12 | Almacenes | `ListWarehouses`, `GetWarehouse`, `CreateWarehouse`, `UpdateWarehouse` | Completa |
| 13 | Consumibles | `ListConsumables`, `GetConsumable`, `CreateConsumable`, `UpdateConsumable` | Con faltante: solo existe `stock_min`, sin reglas de reposición |
| 14 | Movimientos de stock | `RegisterMovement` | Completa |
| 15 | Almacén móvil de la WO | `RecordOrderConsumable`, `SettleOrderConsumable` | Completa (sigue el flujo real de Seam: carga al almacén móvil y liquidación al terminar) |
| 16 | Alertas de stock mínimo | `ListLowStock` | Completa |

## 3. Pedidos al equipo de WO

**Resueltos con el commit `25e7fa7`**
1. Activar / desactivar plantilla: ya existe el campo `enabled` (solo la versión `latest`; abrir una orden con una plantilla deshabilitada se rechaza).
2. Ejecución de bloques: ya existe (confirmar cabecera, iniciar / guardar / completar bloque, con su máquina de estados y predecesores).
3. Consumo y devolución en la orden: ya existe (consumos por orden y liquidación al terminar).

**Abiertos: necesitamos su decisión**
| # | Pedido | Por qué importa |
|---|---|---|
| A | **Prioridad y ubicación** de la WO: ¿existirán o se difieren? | El correo las pide como filtro; hoy no son campos |
| B | **Pausar, cancelar y aprobar**: ¿serán estados de plantilla o transiciones nuevas? | Hoy una orden solo avanza |
| C | **Código de tercerizado** y **disponibilidad de cuadrillas** | Faltan los campos |
| D | **Cómo resolver un usuario por id o email** desde fuera de la interfaz | Los usuarios viven en las cuentas de Seam, no en xolu |
| E | **Alertas de stock mínimo**: ¿deben excluir los almacenes móviles y los consumibles deshabilitados? | Seam hoy los incluye |
| F | **Reasignar una orden**: ¿qué regla aplica (estado, quién puede)? | molu la permite mientras la orden no esté terminada |
| G | **Firma del técnico**: el avance ya no la pide. ¿Dónde se recoge ahora? | `GetOrder` solo la muestra para órdenes antiguas |
| H | **Órdenes antiguas** (abiertas antes del modelo de ejecución, sin máquina propia): la pantalla nueva no las ejecuta. ¿Se migran? | Hay datos de demostración en esa situación |
| I | **Notas por función** cuando suban la versión estable (pidió Horacio) | Para documentar cada función |

**Para decidir con Horacio (no es de WO):** la identidad de quien actúa. Hoy el `user_id` lo informa el cliente MCP sin verificarse (Seam lo toma de la sesión). Hay que definir si el Hub propagará la identidad del usuario autenticado.

## 4. Diferencias con Seam que se corrigieron
Al leer el código nuevo de Seam y probar contra él se alinearon, entre otras cosas: la máquina de estados propia de cada orden con plantilla, el responsable por bloque al crear, la cabecera confirmada antes de ejecutar, el avance que exige todos los bloques completados, el nombre del almacén móvil y la verificación de que la plantilla sea la última versión y esté habilitada. Con xolu real se descubrió además que una orden sin bloques ni almacén móvil no puede crearse con un `Commit` vacío (`XOLU-CM003`); molu la crea con una escritura simple.

## 5. Cómo se verificó
- **Pruebas contra xolu real:** 18 pruebas pasan (`go test` con `MOLU_REAL_XOLU_URL`), cubren el ciclo completo de una orden con bloques y consumibles.
- **Contra Seam real, rama `work_orders` `25e7fa7` (26-nov):** escenario de punta a punta con un cliente MCP y la interfaz de Seam alternándose sobre las mismas órdenes. **50 de 51 comprobaciones pasan**: Seam y molu se alternan para confirmar la cabecera, cargar insumos, ejecutar bloques, avanzar y liquidar, y el stock por almacén cuadra al final; deshabilitar una plantilla desde cualquiera de los dos se ve del otro lado. La comprobación restante fue una expectativa mía equivocada: Seam rechaza abrir una orden con una plantilla deshabilitada con 400 (el selector de plantillas la excluye antes de llegar al 409 que figura en su código), no con 409.
- **Contra Seam 0.4.5 (órdenes sin plantilla):** 9 de 9.

## 6. Límites conocidos
- `user_id` sin verificar (ver §3).
- Seam todavía no tiene evidencias, prioridad, ubicación ni pausa/cancelación; molu no puede ofrecer lo que el módulo real no define.
- No se probó crear una orden desde el formulario de Seam con una plantilla sin bloques ni insumos (por lectura de su código enviaría un `Commit` sin escrituras, que xolu rechaza; no se comprobó en vivo).
- Las pruebas de punta a punta con Seam se hicieron contra instancias locales con datos de demostración.

---

## Mensaje para Slack (`#seam-cure-ai-subsystem-mcp`)

> **Work Orders por MCP, cierre:** las **16 funciones del correo de Granero ya tienen función MCP** (33 funciones `work_orders.*`). **9 están completas y 7 con un faltante que depende de Seam**, no de molu. La que faltaba (activar/desactivar plantilla) ya la implementó WO con el commit `25e7fa7`, y molu ya la usa.
>
> Probado contra Seam real (rama `work_orders` `25e7fa7`): **50 de 51 comprobaciones** de punta a punta; Seam y molu se alternan sobre las mismas órdenes (cabecera, bloques, consumos, liquidación) y el stock cuadra. Contra Seam 0.4.5 (órdenes sin plantilla): 9 de 9.
>
> **Pedidos abiertos para WO** (detalle en `specs/012-work-orders-mcp-coverage/informe-brechas.md`): prioridad y ubicación de la orden; pausar, cancelar y aprobar; código de tercerizado y disponibilidad de cuadrillas; cómo resolver un usuario por id o email; si las alertas de stock mínimo deben excluir almacenes móviles; regla de reasignación; dónde queda la firma del técnico; qué hacer con las órdenes antiguas que la pantalla nueva no ejecuta.
>
> **Para decidir con Horacio:** hoy el `user_id` de quien actúa lo informa el cliente sin verificarse; ¿el Hub propaga la identidad del usuario autenticado?
