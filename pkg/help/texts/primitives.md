summary: las primitivas genéricas: para qué sirven y cuándo no usarlas para escribir
# Primitivas genéricas

`describe` lista las entidades y sus campos; `list`, `get` y `query` leen; `create` y `update` escriben; `walk`, `machine_state` y `machine_history` mueven y consultan máquinas de estados; `cal_*` maneja calendarios. El argumento que nombra la entidad es `entity_type`.

## Cuándo usarlas
- Para **consultar** datos que ninguna función de dominio devuelve.
- Para entender un dato antes de operar con una función de dominio.

## Cuándo no
- **No las uses para crear ni modificar registros que una función de dominio sabe gestionar** (órdenes, incidencias, movimientos de stock...). `create` y `update` guardan lo que reciben sin validar: un registro creado así puede quedar incompleto (sin máquina de estados, sin registros asociados) y no poder usarse después.
- Algunos valores se guardan escalados (por ejemplo cantidades en centésimas de unidad: un 300 son 3 unidades). Las funciones de dominio los convierten; las primitivas no.
