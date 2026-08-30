# molu

An MCP sidecar for [xolu](https://github.com/ha1tch/xolu). molu reads the live schema of a xolu instance, exposes it as [Model Context Protocol](https://modelcontextprotocol.io) tools, and lets AI agents perform structured business operations against the substrate with the guarantees xolu already provides.

molu is agnostic with respect to vertical application. Domain-specific functions are published to a companion component — the **molu hub** — by domain applications that own their own business logic. molu discovers those functions and presents them to the agent alongside its generic primitives.

**Status:** pre-implementation. This repository currently holds the specification.

## Specification

The specification is in three parts, each self-contained.

- [**Part 1 — Motivation and design**](./molu-part1-motivation-and-design.md). Why molu exists, what problem it solves, and how the pieces fit together. Start here.

- [**Part 2 — The MCP frontend**](./molu-part2-mcp-frontend.md). The specification of molu itself: internal architecture, the semantic map built from the xolu schema, the thirteen generic MCP tools, event-free schema refresh, xolu health probing, MCP transports, configuration, availability behaviour.

- [**Part 3 — The molu hub**](./molu-part3-hub.md). The specification of the hub — protocol and reference implementation. The function contract, publisher and consumer protocols, authentication and namespaces, memory and xolu-backed storage backends.

## License

Apache 2.0. See [LICENSE](https://www.apache.org/licenses/LICENSE-2.0).
