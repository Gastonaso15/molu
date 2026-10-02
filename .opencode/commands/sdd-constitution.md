---
description: SDD · Propone o revisa la constitución del proyecto (principios innegociables)
agent: plan
---
Vamos a crear (o revisar, si ya existe) docs/constitution.md adaptada a Molu. Usa la skill sdd.
Antes de proponer nada, lee AGENTS.md, MEMORY.md y el código actual del proyecto en Go.
Contexto adicional: $ARGUMENTS

Revisa y propón los principios innegociables, cortos y verificables, que cubran:
simplicidad del stack en Go, ausencia de data races (`-race`), relación entre spec y código,
separación entre lógica pura y llamadas de red/I/O (cliente xolu y transportes MCP),
política de tests con `go test -v -race`, diseño agnóstico al dominio e idioma de código y textos.

NO sobreescribas el archivo directamente sin mi aprobación: muestra la propuesta y espera.
