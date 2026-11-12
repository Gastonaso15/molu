# Spec 010 — Publicador de Issue Tracker

Estado: borrador

## Contexto y objetivo
El equipo de Issue Tracker requiere exponer sus capacidades de negocio como herramientas MCP dentro del catálogo del Hub de Molu. Para mantener la arquitectura desacoplada de la Constitución (§3.1) y respetar la estricta neutralidad de dominio del sistema, se implementa un servicio publicador independiente (`bin/publisher-issues`) que:
1. Registra dinámicamente sus herramientas en el Hub Standalone con namespace `issues.*` y esquemas formales JSON.
2. Atiende llamadas despachadas por el Execution Bridge de Molu.
3. Se comunica con el sustrato xolu para gestionar el ciclo de vida de incidencias (FSM `issue_lifecycle` de 8 estados) y persistencia en la colección `issues`.
4. Resuelve la criticidad de activos vinculados y computa los plazos SLA correspondientes (primera respuesta y resolución).
5. Preserva la taxonomía tipada de errores de Molu (`NOT_FOUND`, `CONTRACT_VIOLATION`, `SUBSTRATE_UNAVAILABLE` e `INVALID_TRANSITION` verbatim).

## Usuarios / Actores
- **Agente de IA (Cliente MCP)**: Invoca las herramientas `issues.CreateIssue`, `issues.AdvanceIssue` e `issues.GetIssue` a través de Molu Frontend.
- **Molu Frontend (Execution Bridge)**: Valida esquemas, gestiona tokens (`token_ref: "issues:api-token"`) y despacha las peticiones HTTP hacia el publicador de Issue Tracker.
- **Hub Standalone (`bin/hub`)**: Expone el catálogo y admite/puntúa las herramientas mediante el proveedor RPI configurado.
- **Publicador de Issue Tracker (`bin/publisher-issues`)**: Servicio HTTP desacoplado que ejecuta la lógica de dominio e interactúa con xolu.
- **Sustrato xolu**: Backend operacional que gestiona la persistencia de entidades (`issues`, `assets`) y la ejecución de transiciones FSM.

## Historias de usuario
- **HU-1**: Como **publicador de Issue Tracker**, quiero registrar mis funciones (`issues.CreateIssue`, `issues.AdvanceIssue`, `issues.GetIssue`) en el Hub remoto al iniciar, enviando sus contratos JSON, endpoints y requerimientos de autenticación.
- **HU-2**: Como **agente de IA**, quiero invocar `issues.CreateIssue` indicando título, descripción, prioridad y activo asociado, y recibir la incidencia creada en xolu con su estado inicial (`reported`), ID de máquina FSM y plazos SLA calculados.
- **HU-3**: Como **agente de IA**, quiero invocar `issues.AdvanceIssue` hacia un estado no permitido por las reglas de la FSM, recibiendo un error tipado `INVALID_TRANSITION` con el diagnóstico exacto de la FSM de xolu preservado verbatim.
- **HU-4**: Como **agente de IA**, quiero consultar una incidencia inexistente con `issues.GetIssue` y recibir el código tipado `NOT_FOUND`.

## Ejes de Control Involucrados
- **Eje 1 (Qué se ofrece)**: El publicador define formalmente sus herramientas con el namespace `issues.*` y las registra en el Hub Standalone vía HTTP. El proveedor RPI filtra qué herramientas se admiten y puntúan para cada tenant y rol.
- **Eje 2 (Qué puede ejecutarse)**: Al invocar `issues.AdvanceIssue`, el publicador delega la transición a la FSM en xolu (`walk`). Las transiciones definidas en la FSM deciden la validez de la operación y cualquier rechazo viaja verbatim hacia el agente.

## Convención de Nombres de Herramienta
- **Funciones de dominio**: TODAS las herramientas expuestas por este publicador llevan el namespace `issues.` (e.g. `issues.CreateIssue`, `issues.GetIssue`, `issues.AdvanceIssue`).
- Ninguna función colisiona con primitivos genéricos ni omite el prefijo de namespace.

## Definiciones
- **Incidencia (Issue)**: Registro de evento operativo con ciclo de vida de 8 estados (`reported`, `triaged`, `in_progress`, `on_hold`, `resolved`, `closed`, `rejected`, `duplicate`).
- **SLA**: Plazos límites calculados sumando minutos a la fecha actual UTC según la prioridad (`critical`, `high`, `medium`, `low`) y la criticidad del activo (`critical` vs `normal`).

## Requisitos funcionales (en notación EARS)

### Dominio y Servicio de Issue Tracker
- **RF-1**: CUANDO el servicio recibe una solicitud de creación de incidencia con payload válido y autenticación válida, EL SISTEMA:
  1. Valida los campos obligatorios (`title` no vacío, enums válidos de `priority` y `severity`).
  2. Si `asset_id` está presente, consulta la criticidad del activo en xolu (`critical` vs `normal`).
  3. Calcula los plazos SLA en UTC (`sla_first_response_deadline` y `sla_resolution_deadline`).
  4. Garantiza la definición de FSM `issue_lifecycle` e instancia una máquina en xolu en estado inicial `reported`.
  5. Crea la entidad en la colección `issues` de xolu.
  6. Retorna los datos estructurados de la incidencia creada con HTTP 200 OK.
- **RF-2**: CUANDO el servicio recibe una consulta `GetIssue` para un identificador inexistente, EL SISTEMA retorna `NOT_FOUND` (HTTP 404).
- **RF-3**: CUANDO el servicio recibe una solicitud `AdvanceIssue` y la transición es rechazada por la FSM de xolu, EL SISTEMA retorna HTTP 409 con el error tipado `XOLU-INVALID_TRANSITION` preservando el diagnóstico verbatim.
- **RF-4**: CUANDO el servicio recibe una solicitud `AdvanceIssue` con transición `start` y el campo `first_response_at` no estaba registrado, EL SISTEMA persiste la marca de tiempo de primera respuesta y el actor correspondiente.

### Servidor HTTP y Registro
- **RF-5**: CUANDO el binario `cmd/publisher-issues` inicia, EL SISTEMA carga la configuración de variables `MOLU_PUBLISHER_ISSUES_*`, inicia un servidor HTTP en el puerto configurado (default `:8093`) y registra sus herramientas en el Hub remoto especificado por `MOLU_PUBLISHER_ISSUES_HUB_URL`.
- **RF-6**: CUANDO el publicador recibe una petición sin la cabecera `Authorization: Bearer <token>` esperada o con token inválido, EL SISTEMA retorna HTTP 401 Unauthorized.
- **RF-7**: SI el sustrato xolu no está disponible durante cualquier operación, ENTONCES EL SISTEMA retorna HTTP 503 con código `XOLU-SUBSTRATE_UNAVAILABLE` y metadatos de reintento.
- **RF-8**: CUANDO el publicador se apaga ordenadamente (SIGINT/SIGTERM), EL SISTEMA desregistra sus funciones del Hub mediante llamada a `/api/v1/registry/unregister`.

### Concurrencia y Neutralidad
- **RF-9**: EL SISTEMA garantiza ausencia total de data races en todas las peticiones concurrentes (`go test -v -race ./...`).
- **RF-10**: EL SISTEMA mantiene neutralidad estricta de dominio en el código base, constantes y configuración por defecto (cero ocurrencias del término restringido "Seam").

## Requisitos no funcionales
- Concurrencia segura y pase obligatorio de `go test -v -race ./...`.
- Neutralidad de dominio: prohibido usar el término restringido en código, constantes o defaults.
- Compatibilidad Go 1.27+.

## Criterios de finalización
- Implementación de paquetes en `pkg/publisher/issues/` (tipos, servicio, servidor HTTP y registrador).
- Binario ejecutable `bin/publisher-issues` (`cmd/publisher-issues/main.go`).
- Pruebas unitarias y de integración cubiertas bajo `go test -v -race ./...`.
- Neutralidad de dominio verificada con cero coincidencias.

