# Resumen y Análisis Arquitectónico Integral de Molu

**Versión de la especificación:** draft-02  
**Repositorios:** `github.com/ha1tch/molu` | `github.com/ha1tch/molu-hub`  
**Substrato objetivo:** `xolu` (≥ v0.15.0)  
**Estándar de interoperabilidad:** Model Context Protocol (MCP)  

---

## 1. Introducción y Visión General

**Molu** es una arquitectura de integración agéntica y un servidor *sidecar* de **Model Context Protocol (MCP)** diseñado para bases de datos operacionales con esquemas descubribles en tiempo de ejecución (con el motor **xolu** como implementación de referencia). 

A diferencia de los enfoques tradicionales basados en documentación estática o llamadas desestructuradas a APIs REST, Molu actúa como una capa intermedia semántica e inteligente que:
1. **Lee y mapea el esquema operacional vivo** del substrato (entidades, definiciones FSM, generadores/secuencias y definiciones de eventos).
2. **Descubre funciones de dominio** publicadas dinámicamente por aplicaciones de negocio a través de un catálogo desacoplado (**Molu Hub**).
3. **Expone una superficie uniforme de herramientas MCP** para que agentes de inteligencia artificial (como Claude, Copilot, Cursor u orquestadores autónomos) ejecuten operaciones de negocio estructuradas con garantías transaccionales, tipado estricto y validación formal de estados.

---

## 2. Parte 1 — Motivación, Filosofía y Diseño del Sistema

### 2.1. El Problema de la Interacción Agente-Datos
El despliegue de agentes de IA para interactuar con sistemas transaccionales críticos (facturación, reservas, gestión de inventario, cambio de estados en tickets) enfrenta dos limitaciones estructurales en los patrones existentes:

1. **Exposición Directa de APIs REST/GraphQL:**
   - Obliga al LLM a inferir tipos, resolver claves foráneas manualmente y respetar reglas de negocio implícitas.
   - Cualquier error en la construcción del JSON o violación de integridad puede corromper datos.
   - Un cambio en la API rompe de inmediato la comprensión del agente.
2. **Generación Aumentada por Recuperación (RAG) sobre Documentación:**
   - Opera sobre texto estático o semi-estructurado fragmentado en *chunks*.
   - Es un paradigma **estrictamente de sólo lectura** (`Read-Only`).
   - Un agente puede "saber" cómo se factura un pedido según un PDF, pero no puede ejecutar la facturación con garantías atómicas.

### 2.2. El Enfoque "Post-RAG" de Molu
Molu introduce un cambio de paradigma estructural frente a RAG tradicional:

| Eje de Comparación | RAG Tradicional | Arquitectura Molu |
| :--- | :--- | :--- |
| **Fuente de Verdad** | *Corpus* documental estático (PDFs, Markdown, Wikis) | Esquema ejecutable en vivo leído directamente del substrato |
| **Procesamiento** | Fragmentación (*chunking*) y ranking por similitud vectorial | Mapeo semántico estructurado, formal y tipado |
| **Naturaleza Operativa** | Sólo lectura (`Read-Only`) | Lectura y Escritura transaccional (`Read-Write`) |
| **Sincronización (ETL)** | Indexación periódica fuera de línea | ETL en vivo; cambios de esquema se reflejan por sondeo en caliente |
| **Capacidad Agéntica** | Respuestas textuales informativas | Ejecución de transiciones de estado, secuencias y mutaciones atómicas |

### 2.3. Topología de Tres Piezas Desacopladas

El ecosistema Molu se organiza en tres componentes independientes:

```
┌───────────────────────────────────────────────────────────┐
│                    AGENTE DE IA (LLM)                     │
│               (Claude, Cursor, Copilot, etc.)             │
└─────────────────────────────┬─────────────────────────────┘
                              │ JSON-RPC 2.0 (MCP)
                              ▼
┌───────────────────────────────────────────────────────────┐
│                    MOLU (MCP Frontend)                    │
│  - Servidor MCP sin estado (Stateless)                    │
│  - Sostiene el Mapa Semántico en memoria (sync.RWMutex)   │
│  - Expone 13 herramientas genéricas + Funciones de Dominio│
│  - Health Probe & Gated Dispatch hacia el substrato       │
└──────────────┬─────────────────────────────▲──────────────┘
               │                             │
               │ xolu client (HTTP)          │ HTTP Consumer Protocol
               ▼                             │ (/catalogue)
┌──────────────────────────────┐ ┌───────────┴──────────────┐
│        XOLU SUBSTRATE        │ │         MOLU HUB         │
│  (Base de Datos Operacional) │ │   (Catálogo de Dominio)  │
│  - Esquemas JSON & OQL       │ │  - Registro de contratos │
│  - Motor FSM, Cal & Grafos   │ │  - Liveness / Heartbeats │
│  - Control de Acceso y RBAC  │ │  - Sin lógica de negocio │
└──────────────▲───────────────┘ └───────────▲──────────────┘
               │                             │
               │ Ejecución de Negocio        │ HTTP Publisher Protocol
               │ (xolu client)               │ (/publish, /heartbeat)
               └─────────────────────────────┴──────────────┐
                                                            │
                                             ┌──────────────┴──────────────┐
                                             │     DOMAIN APPLICATION      │
                                             │  (ERP, CRM, Facturación)    │
                                             │  - Dueña de reglas de negocio│
                                             │  - Publica contratos al Hub │
                                             └─────────────────────────────┘
```

1. **Molu (Frontend MCP):** Proceso Go sin estado que habla con el agente vía MCP. Lee los esquemas de xolu y consulta el Hub para exponer herramientas.
2. **Molu Hub:** Catálogo desacoplado de funciones de dominio. No contiene lógica de negocio; sólo valida y mantiene contratos de funciones publicados por aplicaciones.
3. **Aplicación de Dominio:** Dueña del negocio. Ejecuta sobre xolu y publica sus funciones públicas al Hub. Nunca habla directamente con Molu ni con el agente.

### 2.4. Los Dos Ejes de Control
El sistema establece un doble cerco de seguridad independiente y complementario:
- **El Hub decide qué se ofrece:** La aplicación de dominio expone al Hub únicamente las funciones autorizadas para consumo agéntico. Lo no publicado es invisible para Molu y el LLM.
- **El Substrato decide qué puede ejecutarse:** Cada llamada a una herramienta o transición FSM está gobernada por las máquinas de estado, permisos de tenant y guardas del substrato xolu. Si una transición viola una precondición, xolu rechaza la mutación.

### 2.5. Principios de Diseño
- **Stateless (Sin Estado Persistente):** Toda la información de sesión y mapas semánticos reside en memoria. Reiniciar Molu no causa pérdida de datos.
- **Contract-First:** Interfaces tipadas mediante JSON Schema formal en todos los límites del sistema.
- **Offline-Capable:** No requiere conexión a nubes externas, APIs de LLMs propietarias ni servicios centralizados de autenticación.
- **Domain-Agnostic:** Ninguna palabra del negocio (*factura*, *ticket*, *paciente*) está cableada en el código fuente de Molu. Todo es conducido por el esquema.

---

## 3. Parte 2 — Especificación del Frontend MCP (`molu`)

### 3.1. Arquitectura Interna de Paquetes
El binario Molu sigue un flujo unidireccional acíclico: `mcp → semantic → exec → xolu client`.

```
molu/
├── cmd/molu/           # Entrypoint, banderas CLI, inicialización y graceful shutdown
├── pkg/mcp/            # Servidor MCP: registro de tools, dispatch JSON-RPC, stdio / HTTP
├── pkg/semantic/       # Mapa semántico: lector de esquemas xolu, resolución y refresco
├── pkg/catalogue/      # Cliente del Hub: descubrimiento de funciones y refresco
├── pkg/exec/           # Capa de ejecución: validación de contratos, logs slog y errores
├── pkg/health/         # Sonda de salud de xolu (ping/pong, backoff, gated dispatch)
├── pkg/config/         # Gestión de configuración unificada (ENV + YAML)
└── pkg/obs/            # Observabilidad: logging estructurado, métricas Prometheus
```

### 3.2. El Mapa Semántico (`SemanticMap`)
Estructura en memoria protegida por un `sync.RWMutex`, reconstruida atómicamente en cada intervalo de sondeo (`MOLU_FRONT_SCHEMA_POLL_INTERVAL`, default 60s):
- **Entidades:** Esquemas JSON Schema, campos regulares y campos `REF` (referencias entre entidades).
- **Máquinas de Estado (FSM):** Estados, transiciones (`from`, `input`, `to`), expresiones de guarda T-SQL, operaciones `SET` y salidas Mealy.
- **Generadores y Secuencias:** Identificadores autoincrementales, UUIDs, ULIDs y CUIDs.
- **Eventos:** Tipos de eventos, fuentes de *latch* y acciones destino.

### 3.3. Superficie de 13 Herramientas Genéricas MCP

| Herramienta | Descripción y Propósito | Endpoint de Despacho en xolu |
| :--- | :--- | :--- |
| `describe` | Lectura estructural del mapa semántico (entidades, FSMs, generadores, eventos) | In-memory `SemanticMap` |
| `get` | Obtención de una entidad por su tipo e identificador | `GET /api/v1/{entity_type}/{id}` |
| `list` | Listado de entidades con filtros de igualdad, paginación (`limit`, `offset`) | `GET /api/v1/{entity_type}` |
| `create` | Creación de entidad validada contra JSON Schema previo al envío | `POST /api/v1/{entity_type}` |
| `update` | Modificación parcial de entidad con control de concurrencia optimista (`version`) | `PATCH /api/v1/{entity_type}/{id}` |
| `query` | Ejecución de consultas estructuradas en lenguajes nativos OQL (tablas) o Sulpher (grafos) | xolu Query Surface (`POST /api/v1/query`) |
| `walk` | Avance de máquina de estados mediante un `input` y un `payload` evaluado en guardas | `POST /api/v2/fsm/machine/{id}/walk` |
| `machine_state` | Consulta del estado actual, variables y estado terminal de una FSM | `GET /api/v2/fsm/machine/{id}/state` & `/vars` |
| `machine_history` | Historial cronológico de transiciones y pasos ejecutados por la máquina | `GET /api/v2/fsm/machine/{id}/history` |
| `cal_check` | Comprobación de disponibilidad sobre calendarios en un intervalo (dry-run sin bloqueo) | xolu Cal Check API |
| `cal_openings` | Búsqueda de huecos disponibles según duración, rango y objetivo (`earliest`, etc.) | xolu Cal Openings API |
| `cal_propose` | Creación de una reserva tentativa en el plano propuesto (*proposed plane*) | xolu Cal Propose API |
| `cal_confirm` | Confirmación y pase de una reserva al plano vinculante definitivo (*binding plane*) | xolu Cal Confirm API |

### 3.4. Sonda de Salud (`Health Probe`) y *Gated Dispatch*
Molu ejecuta una goroutine en segundo plano que sondea periódicamente `GET /ready` de xolu:
- **Cadencia normal:** Cada `MOLU_FRONT_PING_INTERVAL` (30s) con timeout de 5s.
- **Frescura de Pong (`MOLU_FRONT_PONG_FRESHNESS` = 45s):** Mientras el último pong exitoso tenga menos de 45s, las herramientas ejecutan de inmediato sin consultar de nuevo a xolu (evita saturar el motor).
- **Retroceso Exponencial en Fallos:** Si un ping falla, `Healthy` pasa a `false` y el intervalo de reintento aumenta exponencialmente desde `MOLU_FRONT_PING_FAIL_FLOOR` (1s) hasta `MOLU_FRONT_PING_FAIL_CEILING` (30s).
- **Gated Dispatch:** Si `Healthy` es falso, ninguna herramienta ni sondeo de refresco toca a xolu. La llamada falla inmediatamente con código `XOLU-MOLU-FRONT-UNAVAILABLE`.

```
                  ┌──────────────────────┐
                  │   GET /ready (Ping)  │
                  └──────────┬───────────┘
                             │
                  ┌──────────┴──────────┐
         Exitoso (200)               Falla / Timeout
                  │                             │
                  ▼                             ▼
        ┌───────────────────┐         ┌───────────────────┐
        │  Healthy = true   │         │  Healthy = false  │
        │ LastPong = now()  │         │ Backoff: 1s -> 30s│
        └─────────┬─────────┘         └─────────┬─────────┘
                  │                             │
      (Llamadas MCP proceden)        (Llamadas MCP rechazadas con
                                     XOLU-MOLU-FRONT-UNAVAILABLE)
```

### 3.5. Códigos de Error Estructurados

| Código de Error | Causa y Contexto |
| :--- | :--- |
| `XOLU-MOLU-FRONT-UNAVAILABLE` | Sonda de salud en rojo; xolu inaccesible |
| `XOLU-MOLU-FRONT-STARTUP` | Servidor en fase de arranque esperando el primer pong exitoso |
| `XOLU-MOLU-FRONT-TIMEOUT` | La llamada al substrato excedió `MOLU_FRONT_CALL_TIMEOUT` |
| `XOLU-MOLU-FRONT-CONTRACT` | Fallo de validación del payload contra el JSON Schema local |
| `XOLU-MOLU-FRONT-HUB-UNAVAILABLE` | Función de dominio solicitada ya no existe en el catálogo del Hub |
| `XOLU-*` *(pass-through)* | Errores nativos de xolu devueltos con código estructurado para razonamiento del agente |

### 3.6. Transportes MCP y Aislamiento de Tenant
- **stdio:** Entrada estándar (`stdin`) y salida estándar (`stdout`) para agentes locales (Claude Desktop, Cursor). Logs emitidos estrictamente por `stderr`.
- **Streamable HTTP:** Protocolo MCP moderno sobre HTTP con streaming JSON/NDJSON para agentes remotos en red.
- **Frontera de Tenant:** Un proceso Molu atiende **exactamente a un tenant** determinado por las credenciales fijas de arranque.

---

## 4. Parte 3 — Especificación del Hub de Molu (`molu-hub`)

### 4.1. Roles y Contrato de Función
El Hub es un catálogo puro donde las aplicaciones de dominio publican funciones mediante un **Contrato Formal**:

```json
{
  "namespace": "billing",
  "name": "CreateInvoice",
  "description": "Genera una factura para el cliente indicado a partir de un pedido aprobado.",
  "input_schema": {
    "type": "object",
    "required": ["customer_id", "order_id"],
    "properties": {
      "customer_id": { "type": "string" },
      "order_id": { "type": "string" }
    }
  },
  "output_schema": {
    "type": "object",
    "properties": {
      "invoice_id": { "type": "string" },
      "total_amount": { "type": "number" }
    }
  },
  "endpoint": "https://billing.internal/agent/create-invoice",
  "auth": {
    "mode": "bearer",
    "header": "Authorization",
    "token_ref": "vault:billing-token"
  },
  "requires_confirmation": true,
  "idempotent": false,
  "cost": "moderate",
  "latency": "sub-second"
}
```

### 4.2. Protocolo del Publisher (Aplicación de Dominio)
1. **`POST /publish`**: Registra o actualiza atómicamente un contrato de función.
2. **`POST /heartbeat`**: Señal de vida emitida cada `MOLU_HUB_HEARTBEAT_INTERVAL` (30s). Si no se reciben heartbeats tras `MOLU_HUB_HEARTBEAT_TIMEOUT` (90s), el Hub expira y elimina todas las funciones del publisher.
3. **`POST /unpublish`**: Retira voluntariamente una o todas las funciones del catálogo.
4. **`GET /whoami`**: Diagnóstico de identidad, rol y namespaces autorizados.

### 4.3. Protocolo del Consumer (Frontend Molu)
1. **`GET /catalogue`**: Obtiene la instantánea completa de funciones registradas.
2. **`GET /catalogue/{namespace}`**: Filtra funciones pertenecientes a un namespace específico.
3. **`GET /catalogue/{namespace}/{name}`**: Retorna el contrato individual de una función específica.

### 4.4. Backends de Almacenamiento
- **`memory` (Predeterminado):** Almacenamiento en memoria con `sync.Map`. En caso de reinicio, el catálogo se limpia y los publishers activos se re-registran automáticamente durante su ciclo habitual de inicio/heartbeat.
- **`xolu` (Opcional):** Persistencia en el substrato xolu bajo la entidad `molu_hub_function` para mantener el catálogo persistente entre reinicios.

---

## 5. Matriz de Síntesis Arquitectónica

```
┌──────────────────────────────────────────────────────────────────────────────────┐
│                             MATRIZ DE COMPONENTES                                │
├────────────────────────┬────────────────────────┬────────────────────────────────┤
│ Característica         │ Molu (MCP Frontend)    │ Molu Hub                       │
├────────────────────────┼────────────────────────┼────────────────────────────────┤
│ Rol Principal          │ Servidor MCP para LLMs │ Catálogo de Funciones de Negocio│
│ Interfaz Externa       │ MCP (JSON-RPC stdio/HTTP)│ HTTP REST (Publisher/Consumer) │
│ Estado Persistente     │ Ninguno (Stateless)    │ Ninguno (Memoria) o xolu DB    │
│ Dependencia del Substrato│ Directa (xolu client) │ Opcional (solo en modo xolu)   │
│ Descubrimiento         │ Sondeo xolu + Sondeo Hub│ Registro activo de Publishers  │
│ Control de Fallos      │ Sonda Health + Backoff │ Expiración por falta de Liveness│
│ Aislamiento de Tenant  │ 1 Proceso = 1 Tenant   │ 1 Proceso = 1 Tenant           │
└────────────────────────┴────────────────────────┴────────────────────────────────┘
```

Este diseño garantiza una separación limpia de responsabilidades: el agente se orienta con el mapa semántico formal, el negocio gobierna su superficie a través del Hub y el motor de base de datos protege la integridad transaccional en todo momento.
