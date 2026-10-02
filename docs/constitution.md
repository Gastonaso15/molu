# Constitución — Molu & Hub (Equipo IA / MCP)

Principios innegociables con estatus normativo. Toda spec, plan técnico y tarea de implementación debe cumplirlos estrictamente.

1. **Go idiomático, concurrencia segura y stack limpio**:
   - Go 1.27+ estándar y SDK oficial de MCP (`github.com/modelcontextprotocol/go-sdk`).
   - Todo código debe compilar limpiamente y pasar `go test -v -race ./...` (cero data races).
2. **La spec manda y piso normativo**:
   - Nada se implementa si no está en la spec activa (`specs/NNN-*/`).
   - Los entregables fijados en el documento *Detalle de Entregables* constituyen el piso mínimo obligatorio e innegociable. Si falta una decisión de diseño, se detiene la ejecución y se consulta.
3. **Núcleo stateless y los dos ejes de control**:
   - Molu y el Hub son stateless en persistencia propia (la información operativa reside en xolu y el catálogo en memoria).
   - **Eje 1 (Qué se ofrece)**: Determinado por el Hub y formalizado mediante la interfaz RPI (`Admit` y `Score`).
   - **Eje 2 (Qué puede ejecutarse)**: Determinado exclusivamente por las FSMs de xolu (invocadas vía el primitivo genérico `walk`, preservando diagnósticos de guardas verbatim).
   - La lógica de mapeo semántico y transformación es pura y desacoplada de red mediante interfaces mockeables.
4. **Taxonomía tipada de resultados obligatoria (6 códigos)**:
   - Se distinguen de forma estricta e independiente seis códigos tipados, **nunca colapsables entre sí ni reportables como error genérico**:
     1. `NOT_OFFERED`: La función/entidad existe, pero no para este contexto (tenant/rol).
     2. `NOT_FOUND`: El objeto direccionado no existe para este tenant.
     3. `EMPTY`: Consulta válida que no coincidió con ningún elemento (nunca usar padding).
     4. `INVALID_TRANSITION`: Transición FSM rechazada por xolu (diagnóstico verbatim de la guarda).
     5. `CONTRACT_VIOLATION`: Entrada incumple el schema JSON del publicador (lista campos infractores).
     6. `SUBSTRATE_UNAVAILABLE`: xolu inalcanzable (incluye último contacto exitoso y próximo reintento).
   - Los errores originados en molu llevan prefijo `XOLU-MOLU-FRONT-*`; los de xolu (`XOLU-*`) se preservan verbatim. Cada código debe contar con prueba de integración dedicada.
5. **Neutralidad de dominio y convención de nombres**:
   - Ni molu ni el Hub deben contener referencias a "Seam" en código, constantes o configuraciones por defecto (son arquitecturas genéricas para cualquier aplicación cliente).
   - **Primitivos genéricos**: NUNCA llevan namespace (e.g. `walk`, `find`, `get`).
   - **Funciones de dominio**: SIEMPRE llevan `namespace.Nombre` (e.g. `work_orders.CreateOrder`).
6. **TDD estricto y documentación verificable**:
   - Flujo TDD tarea a tarea: test unitario o de integración primero (rojo) $\rightarrow$ código mínimo $\rightarrow$ verde con `go test -v -race` $\rightarrow$ parar.
   - Documentación al mismo nivel que el código: Despliegue, Configuración, Runbook operativo y Diagramas de secuencia Mermaid (`.mmd` fuente + `.pdf` renderizado).
   - Convención de idiomas: código, identificadores, firmas MCP y tests en inglés; especificaciones, planes, tareas y documentación en español.
