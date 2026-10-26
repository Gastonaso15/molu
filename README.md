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

## Comandos Principales

- Compilar: `make build`
- Tests: `make test`
- Cobertura: `make coverage`
- Arrancar local: `make run-molu`
- MCP Inspector: `make inspector`
- xolu local con datos de demo: `make xolu-up` (apagar con `make xolu-down`). El `makefile` elige el script según el sistema; se puede forzar con `XOLU_SCRIPT=scripts/<script>`:
  - macOS: [`scripts/levantar-xolu-macos.sh`](./scripts/levantar-xolu-macos.sh) (puerto por defecto 9191)
  - Windows (Git Bash): [`scripts/levantar-xolu-windows.sh`](./scripts/levantar-xolu-windows.sh) (puerto por defecto 9090)
  - Linux y otros: [`scripts/levantar-xolu.sh`](./scripts/levantar-xolu.sh) (puerto por defecto 9090)

  Opciones y variables (`XOLU_SRC`, `XOLU_BIN`, `SEEDS_DIR`) en la cabecera de cada script. Con `make inspector` hay que pasar el puerto si no es el 9090: `make inspector XOLU_URL=http://127.0.0.1:9191`.

## Licencia

Apache 2.0. Ver [LICENSE](https://www.apache.org/licenses/LICENSE-2.0).
