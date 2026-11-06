# Spec 009 — Publicador Piloto de Work Orders (Funciones de Dominio)

Estado: borrador

## Contexto y objetivo
Con la implementación del Hub de herramientas (Spec 005) y el Execution Bridge en Molu (Spec 008), el sistema cuenta con la infraestructura para registrar, descubrir y despachar llamadas a funciones de dominio (`namespace.Nombre`). Sin embargo, para satisfacer el **Criterio 1 de Aceptación Crítica** ("Cliente MCP de terceros sin modificar completa escenario punta a punta con rechazo de FSM visible en el camino") y validar el paquete de trabajo del **Motor de Integración (§2.3.2)**, se requiere un servicio publicador externo real para el dominio piloto de Órdenes de Trabajo (**Work Orders**).

Esta especificación define el servicio publicador piloto `work-orders-publisher`: un componente desacoplado y autónomo que registra sus funciones en el Hub con sus respectivos contratos JSON (esquemas de entrada, endpoints HTTP y metadatos de autenticación con `token_ref`), atiende peticiones HTTP POST reenviadas por el Execution Bridge de Molu, ejecuta las operaciones de negocio interactuando con las FSMs y entidades del sustrato xolu, y devuelve resultados estructurados o errores de taxonomía tipada preservando diagnósticos verbatim.

## Usuarios / Actores
- **Agente de IA (Cliente MCP)**: Invoca herramientas de dominio como `work_orders.CreateOrder`, `work_orders.AdvanceOrder` y `work_orders.GetOrder` a través de Molu Frontend.
- **Molu Frontend (Execution Bridge)**: Valida esquemas, resuelve credenciales locales a partir de `token_ref`, y efectúa la llamada HTTP passthrough hacia el publicador de Work Orders.
- **Hub de Registro**: Catálogo que recibe y mantiene los registros contractuales emitidos por el publicador.
- **Sustrato xolu**: Backend operacional que gestiona las máquinas de estado finito (FSMs) del ciclo de vida de las órdenes y la persistencia de entidades.
- **Operador**: Inicializa y configura el publicador mediante variables de entorno.

## Historias de usuario
- **HU-1**: Como **agente de IA**, quiero crear una orden de trabajo (`work_orders.CreateOrder`) mediante una herramienta MCP y recibir la confirmación con el identificador generado y estado inicial.
- **HU-2**: Como **agente de IA**, quiero hacer avanzar el estado de una orden (`work_orders.AdvanceOrder`), de modo que si la acción viola las guardas de la FSM de xolu, reciba un error tipado `INVALID_TRANSITION` con el diagnóstico exacto de la guarda rechazada.
- **HU-3**: Como **agente de IA**, quiero consultar el estado de una orden (`work_orders.GetOrder`) y recibir sus datos actualizados o un error `NOT_FOUND` si la orden no existe.
- **HU-4**: Como **operador**, quiero que el publicador se auto-registre en el Hub al iniciar, publicando sus contratos de herramientas con autenticación protegida por `token_ref`.

## Ejes de Control Involucrados
- **Eje 1 (Qué se ofrece)**: El publicador define formalmente sus funciones con el namespace `work_orders.*` y las registra en el Hub. El proveedor RPI filtra qué herramientas de este publicador se hacen visibles para el tenant y rol del agente.
- **Eje 2 (Qué puede ejecutarse)**: Al invocar `work_orders.AdvanceOrder`, el publicador delega la transición a la FSM correspondiente en xolu (a través del primitivo `walk`). Las guardas operacionales de la FSM deciden la validez de la transición y cualquier rechazo viaja verbatim hacia el agente.

## Convención de Nombres de Herramienta
- **Funciones de dominio**: TODAS las funciones expuestas por este publicador DEBEN llevar el namespace `work_orders.` (e.g. `work_orders.CreateOrder`, `work_orders.GetOrder`, `work_orders.AdvanceOrder`).
- Ninguna función de este publicador puede colisionar con primitivos genéricos ni omitir el prefijo de namespace.

## Definiciones
- **Work Order**: Entidad operacional que representa una orden de trabajo con identificador, descripción, prioridad, activo asignado y estado FSM.
- **FSM de Ciclo de Vida**: Máquina de estados en xolu que gobierna las transiciones (`creada` $\rightarrow$ `en_progreso` $\rightarrow$ `completada` o `cancelada`) con guardas de validación.
- **Publisher Registration Payload**: Estructura `hub.ToolRegistration` que describe cada función, su `inputSchema`, su endpoint HTTP y sus requisitos de autenticación (`AuthMetadata`).

## Requisitos funcionales (en notación EARS)
- **RF-1**: CUANDO el publicador arranca, EL SISTEMA registra automáticamente sus herramientas (`work_orders.CreateOrder`, `work_orders.GetOrder`, `work_orders.AdvanceOrder`) en el Hub especificando sus contratos JSON, endpoints HTTP relativos/absolutos y autenticación `bearer` con `token_ref: "orders:api-token"`.
- **RF-2**: CUANDO el publicador recibe una petición HTTP POST en `/api/v1/orders/create` con un payload válido y cabecera de autenticación válida, EL SISTEMA crea la entidad y la instancia FSM en xolu y retorna HTTP 200 con el estado y los datos de la orden creada.
- **RF-3**: CUANDO el publicador recibe una petición HTTP POST en `/api/v1/orders/get` para un identificador inexistente, EL SISTEMA retorna HTTP 404 (para que el Execution Bridge de Molu lo mapee a **NOT_FOUND**).
- **RF-4**: CUANDO el publicador recibe una petición HTTP POST en `/api/v1/orders/advance` y la transición es rechazada por una guarda de la FSM de xolu, EL SISTEMA retorna HTTP 409 con el sobre tipado de error xolu (`XOLU-INVALID_TRANSITION`) preservando el diagnóstico verbatim.
- **RF-5**: CUANDO el publicador recibe una petición sin la cabecera de autorización esperada o con un token inválido, EL SISTEMA retorna HTTP 401 Unauthorized.
- **RF-6**: SI el sustrato xolu no está disponible durante la ejecución de una orden, ENTONCES EL SISTEMA retorna HTTP 503 con código `XOLU-SUBSTRATE_UNAVAILABLE` y metadatos de reintento.
- **RF-7**: CUANDO el publicador se apaga ordenadamente (SIGINT/SIGTERM), EL SISTEMA desregistra sus funciones del Hub mediante `registry.unregister`.
- **RF-8**: EL SISTEMA garantiza ausencia total de data races en todas las peticiones concurrentes (`go test -v -race ./...`).
- **RF-9**: EL SISTEMA mantiene neutralidad estricta de dominio en el código base, constantes y configuración por defecto (cero ocurrencias del término restringido).

## Requisitos no funcionales
- **Concurrencia segura**: El publicador HTTP y el cliente de registro deben ser concurrentemente seguros bajo `go test -v -race ./...`.
- **Resiliencia**: Si el Hub o xolu experimentan fallos transitorios durante el registro, el publicador debe aplicar reintentos con backoff exponencial.
- **Observabilidad**: Todas las peticiones deben propagar encabezados de trazabilidad y correlación (`X-Request-ID`, `traceparent`) hacia xolu y generar logs estructurados.

## Casos límite
- Parámetros incompletos en creación: retorno de HTTP 400 antes de contactar a xolu (aunque Molu ya valida el schema antes de enviar).
- Intento de transición inválida desde un estado terminal (e.g. intentar avanzar una orden `cancelada`): xolu rechaza la transición y el publicador retorna el sobre verbatim `XOLU-INVALID_TRANSITION`.
- Consulta de lista de órdenes sin resultados: retorno de `{ "orders": [] }` para activar el mapeo de taxonomía **EMPTY**.

## Fuera de alcance
- Implementación de interfaz gráfica web (el publicador es un servicio backend puramente HTTP / API).
- Métodos de autenticación distintos de `bearer` y `none` para este piloto.

## Criterios de finalización
- Binario o paquete ejecutable `cmd/publisher-orders` (o `pkg/publisher/orders`).
- Registro exitoso de herramientas en el Hub con metadatos completos (`inputSchema`, endpoints, auth).
- Pruebas unitarias de endpoints HTTP con mocks de xolu.
- Pruebas de integración E2E que completen el **Criterio 1**: Cliente MCP $\rightarrow$ Molu $\rightarrow$ Execution Bridge $\rightarrow$ Publicador de Work Orders $\rightarrow$ xolu FSM con rechazo visible en el camino.
- Cumplimiento de cobertura de tests bajo `-race` y actualización en `MEMORY.md`.

