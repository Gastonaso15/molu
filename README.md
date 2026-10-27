# molu

An MCP sidecar for [xolu](https://github.com/ha1tch/xolu). molu reads the live schema of a xolu instance, exposes it as [Model Context Protocol](https://modelcontextprotocol.io) tools, and lets AI agents perform structured business operations against the substrate with the guarantees xolu already provides.

molu is agnostic with respect to vertical application. Domain-specific functions are published to a companion component — the **molu hub** — by domain applications that own their own business logic. molu discovers those functions and presents them to the agent alongside its generic primitives.

---

## Ejecución y Pruebas con MCP Inspector

Para interactuar y depurar las herramientas MCP expuestas por `molu` de forma interactiva, puedes utilizar el [MCP Inspector](https://github.com/modelcontextprotocol/inspector) oficial de Anthropic / MCP.

### Requisitos previos
- **Go 1.27+**
- **Node.js y npm / npx**
- Una instancia de **xolu** accesible (o mock/local).
- Un archivo de token para autenticación con xolu.

### Variables de entorno mínimas
`molu` requiere las siguientes variables de configuración para iniciar:

```bash
export MOLU_FRONT_XOLU_URL="http://localhost:8080"
export MOLU_FRONT_XOLU_AUTH_MODE="bearer"
export MOLU_FRONT_XOLU_TOKEN_FILE="/ruta/a/tu/token"
export MOLU_FRONT_LOG_LEVEL="debug" # Opcional: para ver trazas detalladas
```

*(Si no tienes un archivo de token creado aún, puedes crear uno de prueba temporal con `echo "token-secret" > token.dev` y apuntar `MOLU_FRONT_XOLU_TOKEN_FILE=token.dev`).*

---

### Método 1: Usando el Makefile (Recomendado y Automático)

El Makefile ya incluye valores por defecto para desarrollo local (`http://localhost:9090`) y genera automáticamente un archivo de token temporal (`.token.dev`) si no existe:

```bash
make inspector
```

> **Personalización (opcional):** Si tu xolu corre en otra URL o puerto:
> ```bash
> make inspector XOLU_URL="http://localhost:8080" TOKEN_FILE="/ruta/a/token"
> ```

---

### Método 2: Con `npx` directo apuntando al binario

1. Compila el binario:
   ```bash
   make build
   ```
2. Ejecuta el Inspector pasando las variables con el flag `-e`:
   ```bash
   npx @modelcontextprotocol/inspector \
     -e MOLU_FRONT_XOLU_URL="http://localhost:9090" \
     -e MOLU_FRONT_XOLU_AUTH_MODE="bearer" \
     -e MOLU_FRONT_XOLU_TOKEN_FILE="$(pwd)/.token.dev" \
     ./bin/molu
   ```

---

### Método 3: Ejecutando directamente con `go run`

Sin necesidad de compilar previamente a `bin/`:

```bash
npx @modelcontextprotocol/inspector \
  -e MOLU_FRONT_XOLU_URL="http://localhost:9090" \
  -e MOLU_FRONT_XOLU_AUTH_MODE="bearer" \
  -e MOLU_FRONT_XOLU_TOKEN_FILE="$(pwd)/.token.dev" \
  go run ./cmd/molu
```


---

### Método 4: Desde la UI Web del Inspector

Si ejecutas el Inspector en modo servidor sin comando:

```bash
npx @modelcontextprotocol/inspector
```

Abre en tu navegador la URL indicada (habitualmente `http://localhost:5173`) y completa el formulario de conexión:
- **Transport Type**: `stdio`
- **Command**: `./bin/molu` (o ruta absoluta a tu ejecutable)
- **Arguments**: *(dejar vacío)*
- **Environment Variables**:
  - `MOLU_FRONT_XOLU_URL`: `http://localhost:8080`
  - `MOLU_FRONT_XOLU_AUTH_MODE`: `bearer`
  - `MOLU_FRONT_XOLU_TOKEN_FILE`: `/ruta/a/tu/token`
- Haz clic en **Connect** para explorar primitivos, esquemas y probar llamadas a herramientas.

---

## Especificación

La especificación completa del sistema está organizada en tres partes en el directorio [`docs/`](./docs):

- [**Part 1 — Motivation and design**](./docs/molu-part1-motivation-and-design.md)
- [**Part 2 — The MCP frontend**](./docs/molu-part2-mcp-frontend.md)
- [**Part 3 — The molu hub**](./docs/molu-part3-hub.md)

---

## Primitivas del Sistema

Molu expone 13 primitivas genéricas contra xolu. Estas primitivas son neutrales en cuanto a dominio y **nunca llevan namespace** (a diferencia de las funciones de dominio publicadas al Hub, que siempre llevan el formato `namespace.Nombre`, ej. `work_orders.CreateOrder`).

Las 13 primitivas estándar definidas en §5.1 son:

1. **describe** — Lee mapas semánticos / esquemas operativos del sustrato.
2. **get** — Recupera una entidad específica por tipo e ID.
3. **list** — Enumera entidades de un tipo dado con filtros y paginación opcionales.
4. **create** — Crea una nueva entidad de un tipo dado.
5. **update** — Actualiza campos de una entidad existente con concurrencia optimista.
6. **query** — Ejecuta una consulta OQL o Sulpher contra el sustrato.
7. **walk** — Ejecuta una transición de estado FSM.
8. **machine_state** — Lee el estado actual, variables y estado terminal de un FSM.
9. **machine_history** — Lee el historial de transiciones de un FSM.
10. **cal_check** — Verificación preliminar de disponibilidad a través de calendarios sin reservar.
11. **cal_openings** — Encuentra huecos de tiempo libres a través de calendarios dado duración, rango y objetivo.
12. **cal_propose** — Crea una propuesta de booking en el plano tentativo.
13. **cal_confirm** — Confirma una propuesta de booking en el plano vinculante.

---

## Comandos Principales

- **Compilar**: `make build` (genera `bin/molu`)
- **Tests y cobertura**: `make test` y `make coverage`
- **Arrancar local**: `make run-molu`
- **MCP Inspector**: `make inspector` (usa npx para levantar el inspector conectado a `bin/molu`)
- **xolu local con datos de demo**: `make xolu-up` (apagar con `make xolu-down`). El `makefile` elige el script según el sistema operativo; se puede forzar con `XOLU_SCRIPT=scripts/<script>`:
  - **macOS**: [`scripts/levantar-xolu-macos.sh`](./scripts/levantar-xolu-macos.sh) (puerto por defecto 9191)
  - **Windows (Git Bash)**: [`scripts/levantar-xolu-windows.sh`](./scripts/levantar-xolu-windows.sh) (puerto por defecto 9090)
  - **Linux y otros**: [`scripts/levantar-xolu.sh`](./scripts/levantar-xolu.sh) (puerto por defecto 9090)

  Opciones y variables (`XOLU_SRC`, `XOLU_BIN`, `SEEDS_DIR`) en la cabecera de cada script. Con `make inspector` hay que pasar el puerto si no es el 9090: `make inspector XOLU_URL=http://127.0.0.1:9191`.

  **Guía rápida por SO para ejecutar el programa:**

  - **Linux**: `make build && ./bin/molu` (o `make run-molu`). Asegúrate de tener las variables de entorno configuradas: `MOLU_FRONT_XOLU_URL`, `MOLU_FRONT_XOLU_AUTH_MODE`, `MOLU_FRONT_XOLU_TOKEN_FILE`.
  - **macOS**: Igual que Linux: `make build && ./bin/molu`. El script `levantar-xolu-macos.sh` usa el puerto 9191 por defecto.
  - **Windows**: `make build` desde PowerShell o CMD, luego ejecuta `.\bin\molu.exe`. Las variables de entorno se establecen típicamente con `set MOLU_FRONT_XOLU_URL=...` en CMD o `$env:MOLU_FRONT_XOLU_URL = ...` en PowerShell.

## Licencia

Apache 2.0. Ver [LICENSE](https://www.apache.org/licenses/LICENSE-2.0).
