# Spec 015 — Skills sobre MCP (extensión estándar)

**Estado**: **Pospuesta** (25-nov-2026, decisión del equipo): la extensión exige una revisión nueva del protocolo base, está en beta y no todos los agentes la soportan. Se conserva lo investigado (T1) para retomarla cuando el estándar se estabilice. Sin plan ni tareas; no se escribió código.
**Origen**: respuesta de Horacio a los resultados del benchmark (spec 014): "si no les complica mucho, mejor dejarlo hecho de acuerdo al estándar en lugar de inventar uno nuevo". Anthropic publicó el 24-nov-2026 el artículo *Skills over MCP, tasks and more* (beta pública en Claude y Claude Code).
**Nota de numeración**: `013-openui-integration` ya existía; el benchmark de perfiles pasó a `014-agent-profiles-benchmark` y esta spec es la 015 (renumeradas el 25-nov-2026).

## 1. Objetivo

Que molu entregue a cualquier cliente MCP compatible la guía de uso del dominio (hoy, el SKILL de Work Orders) **como parte del servidor**, usando la extensión estándar de skills de MCP, sin formatos propios y sin depender de que cada usuario copie archivos a su cliente.

## 2. Hechos verificados

### 2.1 Estado actual de molu (leído en el código, 25-nov-2026)
- `pkg/mcp/server.go` implementa el protocolo a mano (JSON-RPC); **`go.mod` no incluye ningún SDK de MCP**, aunque `docs/constitution.md` dice usar `modelcontextprotocol/go-sdk`. Es una discrepancia a resolver o documentar.
- `initialize` responde `protocolVersion: "2024-11-05"` y solo la capacidad `tools` (`listChanged`). Métodos soportados: `initialize`, `notifications/initialized`, `ping`, `tools/list`, `tools/call`.
- No existen `resources/*`, `server/discover` ni el campo `instructions`.
- Neutralidad (AGENTS.md): ni molu ni el Hub pueden contener la palabra "Seam" en código, constantes o configuración por defecto. El contenido de dominio (el SKILL) pertenece al publicador.

### 2.2 El estándar (T1, 25-nov-2026: texto normativo `ext-skills/specification/beta/skills.mdx` e `implementations.md`, leídos por un subagente; el primer tramo devolvió un resumen que contradecía el literal y se descartó; lo siguiente sale del texto literal)
- **Declaración**: en `extensions["io.modelcontextprotocol/skills"]` de las capacidades **en `server/discover`**; además hay que declarar `resources`. **No consta** mención de `initialize`.
- **Revisión mínima**: base **2026-07-28 "o posterior"**; cada petición lleva `params._meta` con `io.modelcontextprotocol/protocolVersion` y datos de cliente. **No consta** compatibilidad con 2024-11-05, 2025-06-18 ni 2025-11-25. **No consta** la definición de `server/discover` en la extensión (vive en la especificación base 2026-07-28, aún sin leer).
- **`skills/list`**: `resultType: "complete"`, `skills[]`, `ttlMs`, `cacheScope` obligatorios; `nextCursor` opcional. Cada `Skill`: `uri` (del `SKILL.md`), `frontmatter` (`name`, `description` y el resto tal cual), `resources` (lista de `{uri, digest, size}` o la cadena `"dynamic"`).
- **`skills/get`**: `params.uri`; resultado con `skill`, `resultType`, `ttlMs`, `cacheScope`.
- **`resources/read`** de archivos de skill: método base; ejemplo con `contents[{uri, mimeType: "text/markdown", text}]`. No consta regla sobre `blob`.
- **`resources/directory/read`**: solo si se declara `directoryRead: true` (opcional, `false` por defecto); no hace falta para un primer corte.
- **Digest**: `sha256:` + 64 hex en minúscula, sobre los bytes crudos del archivo; `size` en bytes. El host verifica digest, tamaño y frontmatter en cada lectura.
- **URI**: `skill://<skill-path>/<file-path>`; el último segmento de `skill-path` debe ser igual a `frontmatter.name`. Un skill se identifica por (servidor, uri).
- **Límites por skill**: 512 entradas y 16 MiB.
- **`resources/list` y plantillas**: solo se exige declarar la capacidad `resources`; no consta que `resources/list` ni `resources/templates` sean obligatorios.
- **Conformidad**: existe una suite (`modelcontextprotocol/conformance`) y verificación en MCP Inspector (`--verify`, sintaxis sin leer). La spec no trae sección de tests.
- **Clientes (beta)**: Claude (claude.ai), **Claude Code desde v2.1.331** (los skills del servidor se ejecutan como `/<servidor>:<skill>`, con aprobación por versión), VS Code/Copilot (1.147+), y otros. **Gemini CLI: no consta.** ChatGPT y Codex: planeado.
- **Go**: el SDK oficial lo tiene "in progress" (PR #1238); hay servidores en beta (GitHub MCP Server, Google MCP Toolbox, gridctl, skillshare, ToolHive, mcpkit, mcpskills). Antes de los SDK se implementaba con handlers propios de `skills/list`, `skills/get` y `resources/directory/read`.

## 3. Preguntas abiertas (bloquean el diseño)

1. ¿La extensión puede declararse con una revisión de protocolo anterior (molu usa `2024-11-05`), o exige la revisión nueva con `server/discover`? **No consta.** Si exige la nueva, ampliar molu a esa revisión es un trabajo mayor que añadir skills.
2. ¿Existe un camino válido para servidores que solo ofrecen `resources` con `skill://`? La overview dice que no se reconocen solo por el esquema, y que se deben implementar `skills/list` y `skills/get`.
3. ¿Qué hace hoy **Claude Code** con la extensión? La fila de Claude Code en la matriz de clientes aparece sin dato de skills en el texto leído; el artículo dice beta pública. Hay que verificarlo con una prueba real.
4. ¿Hay tests de conformidad? MCP Inspector figura con soporte "Partial" (`--verify`); no se leyó.
5. ¿Qué clientes además de Claude lo soportan (Gemini, el cliente de Gastón)? **No consta.** Define si "estándar" resuelve el caso de Gemini o solo el de Claude.

## 4. Diseño tentativo (a confirmar tras resolver §3)

- **Autoría en el publicador**: el SKILL de Work Orders (hoy `specs/014-agent-profiles-benchmark/skill/work-orders-seam/`) lo aporta `publisher-orders` junto a sus funciones; es contenido de dominio y no entra al código de molu ni del Hub.
- **Registro en el Hub**: el publicador registra sus skills (manifiesto y archivos) junto a sus funciones, con el mismo `namespace`; el Hub las cataloga y las filtra por RPI igual que las funciones (Eje 1).
- **Servicio en molu**: molu consulta el catálogo del Hub y atiende `skills/list`, `skills/get` y `resources/read` de `skill://<namespace>/<nombre>/…`, calculando digest y tamaño.
- **Protocolo**: añadir las capacidades sin romper a los clientes actuales de `2024-11-05`. Si hace falta `server/discover`, convivir con `initialize`.
- Alternativa descartada por ahora: una variable `MOLU_FRONT_SKILLS_DIR` con carpetas locales. Es más simple, pero acopla molu a archivos del operador y no sigue el modelo "el publicador es dueño del dominio".

## 5. Requisitos (EARS, borrador)

- **RF-1**: Cuando un cliente consulte `skills/list`, el sistema devolverá un manifiesto conforme a la extensión con una entrada por skill publicada y ofrecida (taxonomía §2.4).
- **RF-2**: Cuando un cliente lea un archivo de una skill con `resources/read`, el sistema devolverá su contenido y el digest coincidirá con el del manifiesto.
- **RF-3**: Si el `uri` no existe, el sistema responderá con el error estándar (`-32602`) y no con un error genérico.
- **RF-4**: Mientras el Hub no ofrezca skills, el sistema no declarará la capacidad y se comportará como hoy.
- **RF-5**: El código de molu y del Hub no contendrá la palabra "Seam"; el contenido de dominio solo existirá en el publicador.
- **RF-6**: Un cliente que no soporte la extensión seguirá funcionando sin cambios.
- **RF-7**: Con la skill servida, la tarea del perfil 3 del benchmark deberá poder repetirse **sin** copiar archivos al cliente.

## 6. Fuera de alcance

Tasks, Elicitation, File Uploads, Tool search y MCP Apps del mismo artículo; skills con ejecución de código o `allowed-tools`; skills dinámicas (`"dynamic"`); autenticación de usuarios.

## 7. Entregables documentales (AGENTS.md §2.3.1)

Actualizar despliegue, configuración (nuevas variables `MOLU_FRONT_*` si las hay), runbook y diagrama de secuencia (`.mmd` y `.pdf`) del flujo publicador → Hub → molu → cliente.

## 8. Resultado de T1 y consecuencia

- §3.1 respondida a medias: la extensión exige la revisión 2026-07-28 con `server/discover` y `_meta` por petición; compatibilidad con revisiones anteriores **no consta**.
- §3.2 respondida: los servidores deben implementar `skills/list` y `skills/get`; un servidor solo con `resources` no se reconoce.
- §3.3: Claude Code lo soporta en beta desde la v2.1.331 (según `implementations.md`); falta probarlo con un servidor real.
- §3.4: existen la suite de conformidad y `--verify` del Inspector.
- §3.5: Gemini CLI no figura.
- **Consecuencia**: hacerlo "según el estándar" implica que molu hable la revisión 2026-07-28 (hoy habla 2024-11-05). Eso es un cambio de protocolo, no solo añadir tres métodos. El trabajo es **mayor**, no medio.
- **Pendiente**: leer la especificación base 2026-07-28 (`server/discover`, `_meta`, modelo sin sesión) para dimensionarlo, y decidir con Horacio entre (A) migrar molu a esa revisión, (B) servidor adicional solo de skills sobre la revisión nueva junto al actual, o (C) interino sin extensión (p. ej. `prompts` o `instructions`, no verificados con ningún cliente).
