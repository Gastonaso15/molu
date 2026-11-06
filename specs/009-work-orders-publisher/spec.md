# Spec 009 — Hub Standalone y Publicador Piloto de Work Orders

Estado: borrador

## Contexto y objetivo
Con la implementación del Hub de herramientas (Spec 005) y el Execution Bridge en Molu (Spec 008), el sistema cuenta con la infraestructura conceptual para registrar, descubrir y despachar llamadas a funciones de dominio (`namespace.Nombre`).

Para materializar la arquitectura real definida en la Constitución (§3.1) y cumplir con el **Criterio 1 de Aceptación Crítica** ("Cliente MCP de terceros sin modificar completa escenario punta a punta con rechazo de FSM visible en el camino"), es imperativo **desacoplar el Hub del binario de Molu** y contar con un publicador externo real:

1. **Hub Standalone (`bin/hub`)**: Un servicio central de catálogo independiente con endpoint de red HTTP/Streamable para registro dinámico de publicadores y consulta de herramientas admitidas por RPI.
2. **Cliente Remoto de Hub en Molu**: Permite a `molu` conectarse al Hub remoto vía HTTP (`MOLU_FRONT_HUB_URL`) manteniendo fallback en memoria cuando no está configurado.
3. **Publicador Piloto de Work Orders (`bin/publisher-orders`)**: Un servicio desacoplado y autónomo que registra sus funciones en el Hub con sus respectivos contratos JSON (esquemas de entrada, endpoints HTTP y metadatos de autenticación con `token_ref`), atiende peticiones HTTP POST reenviadas por el Execution Bridge de Molu, ejecuta las operaciones de negocio interactuando con las FSMs y entidades del sustrato xolu, y devuelve resultados estructurados o errores de taxonomía tipada preservando diagnósticos verbatim.

## Usuarios / Actores
- **Agente de IA (Cliente MCP)**: Invoca herramientas de dominio como `work_orders.CreateOrder`, `work_orders.AdvanceOrder` y `work_orders.GetOrder` a través de Molu Frontend.
- **Molu Frontend (Execution Bridge)**: Consulta al Hub remoto desacoplado, valida esquemas, resuelve credenciales locales a partir de `token_ref`, y efectúa la llamada HTTP passthrough hacia el publicador de Work Orders.
- **Hub Standalone**: Servicio desacoplado que expone endpoints HTTP para registro de publicadores y descubrimiento filtrado por RPI.
- **Publicador de Work Orders**: Servicio HTTP externo que expone la lógica de dominio y se comunica con xolu.
- **Sustrato xolu**: Backend operacional que gestiona las máquinas de estado finito (FSMs) del ciclo de vida de las órdenes y la persistencia de entidades.
- **Operador**: Inicializa y configura los tres servicios mediante variables de entorno.

## Historias de usuario
- **HU-1**: Como **operador**, quiero ejecutar el Hub como un binario y servicio independiente (`bin/hub`), para que los publicadores externos puedan registrar sus herramientas vía HTTP sin interferir con el transporte stdio de `molu`.
- **HU-2**: Como **operador de Molu**, quiero configurar `MOLU_FRONT_HUB_URL` para que `molu` consulte dinámicamente las herramientas y metadatos del Hub remoto.
- **HU-3**: Como **publicador de Work Orders**, quiero registrar mis funciones (`work_orders.CreateOrder`, `work_orders.AdvanceOrder`, `work_orders.GetOrder`) en el Hub remoto al iniciar, enviando sus contratos JSON, endpoints y requerimientos de autenticación.
- **HU-4**: Como **agente de IA**, quiero invocar `work_orders.CreateOrder` y recibir la confirmación de la orden creada en xolu con su estado inicial.
- **HU-5**: Como **agente de IA**, quiero invocar `work_orders.AdvanceOrder` hacia un estado no permitido por las reglas de negocio, recibiendo un error tipado `INVALID_TRANSITION` con el diagnóstico exacto de la guarda de la FSM de xolu preservado verbatim.
- **HU-6**: Como **agente de IA**, quiero consultar una orden inexistente con `work_orders.GetOrder` y recibir el código tipado `NOT_FOUND`.

## Ejes de Control Involucrados
- **Eje 1 (Qué se ofrece)**: El publicador define formalmente sus funciones con el namespace `work_orders.*` y las registra en el Hub Standalone vía HTTP. El proveedor RPI en el Hub filtra qué herramientas se admiten y puntúan para el tenant y rol del agente.
- **Eje 2 (Qué puede ejecutarse)**: Al invocar `work_orders.AdvanceOrder`, el publicador delega la transición a la FSM correspondiente en xolu (a través del primitivo `walk`). Las guardas operacionales de la FSM deciden la validez de la transición y cualquier rechazo viaja verbatim hacia el agente.

## Convención de Nombres de Herramienta
- **Funciones de dominio**: TODAS las funciones expuestas por este publicador DEBEN llevar el namespace `work_orders.` (e.g. `work_orders.CreateOrder`, `work_orders.GetOrder`, `work_orders.AdvanceOrder`).
- Ninguna función de este publicador puede colisionar con primitivos genéricos ni omitir el prefijo de namespace.

## Requisitos funcionales (en notación EARS)

### Fase 1: Hub Standalone y Cliente Remoto
- **RF-1**: CUANDO el binario `cmd/hub` inicia, EL SISTEMA carga la configuración desde `MOLU_HUB_*`, inicializa el registro en memoria con el proveedor RPI configurado, e inicia un servidor HTTP en el puerto indicado por `MOLU_HUB_HTTP_ADDR` (default `:8091`).
- **RF-2**: CUANDO el Hub Standalone recibe una petición HTTP POST en `/api/v1/registry/register` con una definición de herramienta válida (`hub.ToolRegistration`), EL SISTEMA almacena la herramienta en su catálogo y responde HTTP 200 OK; SI el contrato es inválido, retorna HTTP 400 con `CONTRACT_VIOLATION`.
- **RF-3**: CUANDO el Hub Standalone recibe una petición HTTP POST en `/api/v1/registry/unregister`, EL SISTEMA elimina la herramienta y responde HTTP 200 OK; SI la herramienta no existe, retorna HTTP 404 con `NOT_FOUND`.
- **RF-4**: CUANDO el Hub Standalone recibe una petición HTTP POST en `/api/v1/discovery/tools` con un `AdmitContext` válido, EL SISTEMA ejecuta `Admit` y `Score` y retorna HTTP 200 con la lista de herramientas admitidas ordenadas por relevancia; SI el tenant está vacío, retorna HTTP 400 con `CONTRACT_VIOLATION`; SI no hay herramientas admitidas, retorna HTTP 200 con taxonomía `EMPTY`.
- **RF-5**: CUANDO `molu` inicia con `MOLU_FRONT_HUB_URL` configurada, EL SISTEMA utiliza un cliente HTTP remoto para implementar `CatalogLookup` y `ToolDiscoverer`, delegando el descubrimiento y la obtención de metadatos al Hub remoto.

### Fase 2: Publicador de Work Orders
- **RF-6**: CUANDO el servicio `publisher-orders` inicia, EL SISTEMA registra automáticamente sus herramientas (`work_orders.CreateOrder`, `work_orders.GetOrder`, `work_orders.AdvanceOrder`) en el Hub remoto especificado por `MOLU_PUBLISHER_HUB_URL` con sus esquemas JSON, endpoints relativos y autenticación `bearer` con `token_ref: "orders:api-token"`.
- **RF-7**: CUANDO el publicador recibe una petición HTTP POST en `/api/v1/orders/create` con payload válido y autenticación válida, EL SISTEMA crea la entidad y la instancia FSM en xolu y retorna HTTP 200 con el estado y los datos de la orden creada.
- **RF-8**: CUANDO el publicador recibe una petición HTTP POST en `/api/v1/orders/get` para un identificador inexistente, EL SISTEMA retorna HTTP 404 (para que el Execution Bridge de Molu lo mapee a `NOT_FOUND`).
- **RF-9**: CUANDO el publicador recibe una petición HTTP POST en `/api/v1/orders/advance` y la transición es rechazada por una guarda de la FSM de xolu, EL SISTEMA retorna HTTP 409 con el sobre tipado de error xolu (`XOLU-INVALID_TRANSITION`) preservando el diagnóstico verbatim.
- **RF-10**: CUANDO el publicador recibe una petición sin la cabecera de autorización esperada o con token inválido, EL SISTEMA retorna HTTP 401 Unauthorized.
- **RF-11**: SI el sustrato xolu no está disponible durante la ejecución de una orden, ENTONCES EL SISTEMA retorna HTTP 503 con código `XOLU-SUBSTRATE_UNAVAILABLE` y metadatos de reintento.
- **RF-12**: CUANDO el publicador se apaga ordenadamente (SIGINT/SIGTERM), EL SISTEMA desregistra sus funciones del Hub mediante llamada a `/api/v1/registry/unregister`.

### Fase 3: Concurrencia y Neutralidad
- **RF-13**: EL SISTEMA garantiza ausencia total de data races en todas las peticiones concurrentes a través de los tres servicios (`go test -v -race ./...`).
- **RF-14**: EL SISTEMA mantiene neutralidad estricta de dominio en el código base, constantes y configuración por defecto (cero ocurrencias del término restringido "Seam").

## Requisitos no funcionales
- **Concurrencia segura**: El Hub HTTP, el cliente remoto de Molu y el publicador deben ser concurrentemente seguros bajo `go test -v -race ./...`.
- **Resiliencia**: Si el Hub no está listo cuando el publicador o Molu arrancan, aplican reintentos con backoff exponencial.
- **Observabilidad**: Propagación de encabezados de trazabilidad y correlación (`X-Request-ID`, `traceparent`) y métricas de telemetría.

## Casos límite
- Hub Standalone no disponible al iniciar Molu: Molu reporta error de arranque o entra en modo degradado si la configuración lo estipula.
- Registro duplicado en el Hub: Sobrescritura idempotente sin error.
- Intento de transición en FSM de orden cancelada/terminada: Retorno de `XOLU-INVALID_TRANSITION` verbatim.
- Consulta de lista de órdenes vacía: Retorno estructurado con mapeo a `EMPTY`.

## Criterios de finalización
- Binarios ejecutables generados por `make build`:
  - `bin/molu` (frontal MCP)
  - `bin/hub` (catálogo y registro desacoplado)
  - `bin/publisher-orders` (servicio publicador de Work Orders)
- Pruebas unitarias de cada componente con cobertura bajo `-race`.
- Prueba de integración punta a punta del **Criterio 1**:
  - `bin/hub` corriendo en `:8091`.
  - `bin/publisher-orders` corriendo en `:8092` y auto-registrado en el Hub.
  - `bin/molu` consultando al Hub y ejecutando herramientas vía Execution Bridge.
  - Cliente MCP invocando `work_orders.CreateOrder` y luego `work_orders.AdvanceOrder` provocando y verificando un rechazo de guarda de FSM visible verbatim.
- Actualización de `MEMORY.md`.
