summary: cómo ejecutar una orden paso a paso: cabecera, bloques y avance de estado
# Ejecutar una orden

Una orden asignada (o ya reclamada del tablero) se ejecuta en este orden:

1. **`ConfirmExecutionHeader(order_id, user_id, header_values)`** con los ítems `upon_executing` obligatorios (los ves en `GetTemplate`). Se confirma una sola vez; sin esto no arrancan los bloques ni la orden avanza. Si un ítem obligatorio es un dato que solo una persona puede decidir (por ejemplo "recepción confirmada") y el usuario no lo dio, pregúntalo.
2. **Cada bloque**, respetando los predecesores: `ExecuteBlock(order_id, block, intent=start)` y luego `intent=complete` con `values {linea: valor}`. `save` guarda las respuestas sin completar. Los valores se validan por el tipo de la línea: gps `"lat,lng"`, fecha `AAAA-MM-DD`, número, booleano, texto.
3. **`AdvanceOrder(order_id, input=complete, user_id)`** pasa al siguiente estado de la plantilla. Entrar al último estado exige todos los bloques completados. Ya no se pide firma ni se mandan respuestas en el avance. Hay que avanzar una vez por cada salto de estado.

## Reglas que verás en los rechazos
`not_responsible` (el usuario no es el responsable), `header_not_confirmed` (falta el paso 1), `predecessor_pending` (un bloque espera a otro), `invalid_block_state` (el bloque no está en el estado que pide la acción), `blocks_incomplete` (faltan bloques para cerrar), `awaiting_claim` (la orden está en el tablero), `final_state` (la orden ya terminó).

## Órdenes con consumibles
Cargar insumos a la orden y liquidarlos al terminar es parte del flujo: tema `work_orders.inventory`.
