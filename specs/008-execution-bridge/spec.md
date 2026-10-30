# Spec 008 — Execution Bridge para Funciones de Dominio del Hub

Estado: aprobada

## Contexto y objetivo
El servidor MCP **Molu** actualmente despacha llamadas (`tools/call`) a los 13 primitivos genéricos de xolu (Spec 006) y descubre herramientas registradas en el Hub mediante RPI Admit/Score en `tools/list` (Spec 004 / Spec 005). Sin embargo, cuando un agente de IA intenta invocar una función de dominio (`namespace.Nombre`, e.g., `work_orders.CreateOrder`) descubierta a través del Hub, Molu no dispone de un mecanismo de puente de ejecución para despachar la invocación al endpoint HTTP registrado por el publicador.

Esta especificación define el **Execution Bridge** en Molu: el componente encargado de validar la entrada contra el esquema de la función, resolver credenciales de autenticación seguras a partir de referencias (`token_ref`), reenviar la llamada por HTTP passthrough al endpoint del publicador, mapear los fallos a la taxonomía tipada de Molu (preservando el diagnóstico) y retornar la respuesta al agente.

## Usuarios / Actores
- **Agente de IA (cliente MCP)**: Invoca herramientas de dominio (`tools/call` con `namespace.Nombre`) esperando una respuesta homogénea o un error tipado de taxonomía.
- **Publicador de dominio**: Servicio externo que registró la función en el Hub con su contrato JSON (endpoint, schemas, autenticación, flags de confirmación e idempotencia). Recibe la petición HTTP POST desde Molu y devuelve el resultado de negocio.
- **Hub de Seam / Registry**: Catálogo de herramientas que provee las especificaciones contractuales (`ToolRegistration`) de las funciones de dominio.
- **Operador del Sistema**: Configura la resolución de secretos locales y políticas de timeout para llamadas a endpoints de publicadores.

## Historias de usuario
- **HU-1**: Como **agente de IA**, quiero invocar una función de dominio (`namespace.Nombre`) y recibir la respuesta del publicador o un error tipado de taxonomía si los parámetros son inválidos o el publicador no está disponible.
- **HU-2**: Como **agente de IA**, si intento invocar una función de dominio que no existe en el catálogo o no está admitida para mi contexto de tenant/rol, quiero recibir inmediatamente un error `NOT_OFFERED`.
- **HU-3**: Como **publicador**, quiero que Molu valide los parámetros contra mi `input_schema` antes de invocar mi endpoint para proteger mi servicio contra payloads malformados (`CONTRACT_VIOLATION`).
- **HU-4**: Como **operador**, quiero que Molu resuelva tokens de autenticación para los endpoints a partir de `token_ref` locales seguros sin almacenar secretos en el catálogo del Hub.

## Ejes de Control Involucrados
- **Eje 1 (Qué se ofrece)**: Molu verifica que la función solicitada pertenezca al catálogo admitido por el proveedor RPI configurado (`AdmitContext`). Si la función no fue admitida o no existe, se rechaza de inmediato con `NOT_OFFERED`.
- **Eje 2 (Qué puede ejecutarse)**: A diferencia de los primitivos nativos cuyo control de ejecución reside en las FSMs de xolu, las funciones de dominio delegan la ejecución al publicador externo a través de su endpoint HTTP registrado. Si el publicador rechaza la ejecución o la transición de negocio, Molu preserva y mapea la respuesta a la taxonomía tipada.

## Convención de Nombres de Herramienta
- **Primitivos genéricos contra xolu**: NUNCA llevan namespace (`walk`, `get`, `find`, etc.). Siguen siendo ejecutados por el despachador de primitivos nativos (Spec 006).
- **Funciones de dominio del Hub**: SIEMPRE llevan `namespace.Nombre` (e.g. `work_orders.CreateOrder`, `inventory.ReserveStock`). Si una función del Hub intenta registrarse sin namespace o colisiona con un primitivo genérico, es rechazada y nunca despachada al puente.

## Definiciones
- **Domain Function**: Herramienta de dominio publicada en el Hub con nombre compuesto `namespace.Nombre`.
- **Bridge Dispatcher**: Componente ejecutor HTTP en Molu que implementa la interfaz de ejecución para herramientas no primitivas.
- **Token Resolver**: Mecanismo local de resolución de secretos a partir de `token_ref` (e.g. variables de entorno `MOLU_FRONT_TOKEN_<REF>` o archivo mapeado).
- **Invocation Request**: Payload HTTP POST enviado al endpoint del publicador conteniendo los argumentos JSON de la herramienta.

## Requisitos funcionales (en notación EARS)
- RF-1: CUANDO un agente invoca `tools/call` para una función con nombre `namespace.Nombre`, EL SISTEMA verifica si la herramienta está registrada en el catálogo del Hub y admitida en el contexto RPI actual.
- RF-2: SI la función `namespace.Nombre` no existe en el catálogo o fue excluida por el proveedor RPI, ENTONCES EL SISTEMA retorna **NOT_OFFERED** (`XOLU-MOLU-FRONT-NOT_OFFERED`).
- RF-3: CUANDO se invoca una función de dominio admitida, EL SISTEMA valida los argumentos contra el `input_schema` declarado en el contrato de la función.
- RF-4: SI los argumentos no cumplen con el `input_schema`, ENTONCES EL SISTEMA retorna **CONTRACT_VIOLATION** (`XOLU-MOLU-FRONT-CONTRACT_VIOLATION`) detallando los campos infractores en `details.fields[]` sin contactar al publicador.
- RF-5: CUANDO los argumentos son válidos, EL SISTEMA resuelve las credenciales declaradas en `auth` (`mode`: `none`, `bearer`, `apikey`; `token_ref`) utilizando el resolvedor local de secretos y añade los encabezados HTTP correspondientes.
- RF-6: SI el `token_ref` requerido no puede ser resuelto o no existe localmente, ENTONCES EL SISTEMA retorna **CONTRACT_VIOLATION** con diagnóstico de configuración de credenciales sin realizar la petición externa.
- RF-7: CUANDO la petición es enviada al endpoint HTTP del publicador y el publicador responde con HTTP 200/2xx y cuerpo JSON válido, EL SISTEMA retorna el resultado al agente en el formato estándar MCP `CallToolResult`.
- RF-8: SI el endpoint del publicador devuelve HTTP 404, ENTONCES EL SISTEMA retorna **NOT_FOUND** (`XOLU-MOLU-FRONT-NOT_FOUND`).
- RF-9: SI el endpoint del publicador es inalcanzable, agota el timeout (`MOLU_FRONT_DOMAIN_CALL_TIMEOUT`) o responde con HTTP 5xx, ENTONCES EL SISTEMA retorna **SUBSTRATE_UNAVAILABLE** (`XOLU-MOLU-FRONT-SUBSTRATE_UNAVAILABLE`) con los metadatos de reintento correspondientes.
- RF-10: SI el cuerpo de respuesta del publicador indica una respuesta vacía estructurada (e.g. colección sin elementos), ENTONCES EL SISTEMA retorna **EMPTY** (`XOLU-MOLU-FRONT-EMPTY`).
- RF-11: SI el cuerpo de respuesta del publicador contiene un error tipado de xolu (código `XOLU-*`), ENTONCES EL SISTEMA preserva el código verbatim e informa el resultado correspondiente (incluyendo `INVALID_TRANSITION` si proviene de una guarda FSM).
- RF-12: MIENTRAS se ejecutan llamadas concurrentes a diferentes funciones de dominio y primitivos genéricos, EL SISTEMA garantiza ausencia total de data races (`go test -v -race ./...`).
- RF-13: EL SISTEMA mantiene neutralidad de dominio estricta: cero referencias a la palabra "Seam" en el código fuente, constantes, tests y configuraciones por defecto.

## Requisitos no funcionales
- **Concurrencia segura**: Todas las estructuras y llamadas HTTP deben ser concurrentemente seguras y verificadas con `go test -v -race ./...`.
- **Timeouts y cancelación**: Toda petición HTTP al publicador debe respetar el `context.Context` recibido, con un timeout configurable (`MOLU_FRONT_DOMAIN_CALL_TIMEOUT`, por defecto 30s).
- **Observabilidad**: Toda invocación de función de dominio debe registrar métricas Prometheus (contadores de llamadas y latencia con tag `component="molu-front"`, `tool="namespace.Nombre"`) y logs estructurados bajo el dominio de red y aplicación.
- **Aislamiento de red**: Molu solo realiza peticiones salientes hacia endpoints declarados con esquemas válidos (`http://` o `https://`).

## Casos límite
- Publicador tarda más del timeout configurado: cancelación limpia de la conexión HTTP y retorno de `SUBSTRATE_UNAVAILABLE`.
- Respuesta del publicador no es JSON válido (e.g. HTML de error 502 de un reverse proxy): emisión de `SUBSTRATE_UNAVAILABLE` preservando diagnóstico.
- Función con `requires_confirmation: true`: identificación de la directiva (en V1, registro de log/advertencia o passthrough validado).
- Payload vacío en funciones que no requieren parámetros: aceptación limpia si el esquema no define campos obligatorios.

## Fuera de alcance
- Retransmisión automática (retries) en llamadas no idempotentes (`idempotent: false`).
- Protocolos de transporte distintos de HTTP/HTTPS para publicadores (e.g. gRPC o WebSockets).
- Caché de respuestas de funciones de dominio (Molu es estrictamente stateless respecto a datos de publicadores).

## Criterios de finalización
- Implementación de la interfaz `DomainExecutor` / `DomainBridge` integrada en el servidor MCP `pkg/mcp/server.go`.
- Pruebas unitarias de validación de esquemas, resolución de auth y mapeo de errores HTTP hacia la taxonomía tipada.
- Pruebas de integración dedicadas para cada uno de los códigos de la taxonomía aplicables (`NOT_OFFERED`, `NOT_FOUND`, `EMPTY`, `CONTRACT_VIOLATION`, `SUBSTRATE_UNAVAILABLE`).
- Verificación completa con `go test -v -race ./...` pasando en verde sin advertencias.
- Registro actualizado en `MEMORY.md`.

## Dudas resueltas
- [RESUELTO] **Resolución de Secretos (`token_ref`)**: Molu resuelve los `token_ref` declarados en los contratos a través de variables de entorno con el prefijo `MOLU_FRONT_TOKEN_<REF>` donde los caracteres no alfanuméricos se normalizan a guiones bajos en mayúsculas (ej. `vault:token-1` -> `MOLU_FRONT_TOKEN_VAULT_TOKEN_1`), con soporte prioritario para archivos de secretos mediante el sufijo `_FILE` (`MOLU_FRONT_TOKEN_<REF>_FILE`).
- [RESUELTO] **Origen del catálogo para ejecución**: Se inyecta una interfaz de consulta al catálogo (`ToolLookup` o `hub.Registry`) al despachador de dominio en Molu. Antes de invocar el endpoint, Molu valida la admisión RPI mediante `rpi.Offer(ctx, admitCtx, tool)` para garantizar que la herramienta esté autorizada para el tenant y roles de la petición antes de enviarla al publicador.
