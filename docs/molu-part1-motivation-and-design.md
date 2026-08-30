# molu — Part 1: Motivation and Design

Version: draft-02
Status: pre-implementation
Repository: github.com/ha1tch/molu

---

## What molu is

molu is an MCP sidecar for operational databases with discoverable schemas. It reads the live schema of the database it is connected to, translates natural-language intent from AI agents into structured operations against that database, and executes them with the full guarantees the underlying substrate provides.

The reference implementation targets **xolu**, a multi-model operational database with a REST API and formal state-machine primitives. molu is designed to be reusable across any operational database that meets the same discoverability and safety requirements, though xolu is the only such substrate currently documented.

## What MCP is

The **Model Context Protocol** (`modelcontextprotocol.io`) is an open standard, introduced by Anthropic in late 2024 and adopted by major model providers, that defines how AI models communicate with external tools and data sources. It standardises tool discovery, tool invocation, and resource access, so that any MCP-compatible client — Claude, Copilot, Cursor, or any other — can interact with any MCP-compatible server without bespoke integration.

molu implements the MCP server role using the official Go SDK (`github.com/modelcontextprotocol/go-sdk`).

## The problem

AI agents increasingly need to operate on live business data — not merely consult it. Preparing an invoice, advancing a workflow to the next state, reserving inventory, closing a service ticket: these are operations with real consequences that must be safe, traceable, and consistent with the rules of the domain they touch.

Two established approaches fall short of this need.

**Direct API exposure.** Handing an agent raw access to REST or GraphQL endpoints makes it responsible for understanding schemas, resolving foreign keys, respecting business rules, and constructing transactionally correct payloads. Every mistake compounds. Every change to the API breaks the agent's understanding. There is no shared model of what is safe, what requires authorisation, or what has already happened.

**Retrieval-augmented generation over documentation.** RAG systems can tell an agent that an invoicing process exists, but they cannot execute it. They operate on static or slow-moving corpora, ranked by relevance, injected into context. They are read-only by nature. The agent consumes knowledge; it does not act on the world.

What is missing is an intermediate layer that understands the operational model of the application it sits in front of, exposes that model as MCP tools with meaningful semantics, and executes those tools with the guarantees the underlying substrate provides. molu is that layer.

## Why this is post-RAG

molu is not a retrieval system. It differs from RAG on every axis that matters:

- **No corpus.** The substrate's schema is a live executable specification, not a document collection. Entity types, reference relationships, state-machine definitions, sequences, event subscriptions — these describe exactly what exists and exactly what can happen to it.

- **No chunking or ranking.** The schema is complete and precise. molu reads it in full at startup and refreshes it on change. There is no relevance-ranking problem because the schema is structured, not unstructured.

- **Read-write.** molu executes writes, commits, state-machine transitions, and sequence increments. Instructions with real consequences produce real mutations.

- **Live ETL.** molu translates natural-language intent into structured substrate operations in real time. Schema changes are immediately reflected in molu's behaviour, without redeployment.

- **Agentic.** A single natural-language request may require multi-step reasoning: entity lookup, reference resolution, state assessment, transaction construction, result verification. molu plans and executes that sequence autonomously, within bounds the domain has set.

The grounding that makes this reliable is structural, not statistical. The substrate's schema is formal and machine-readable. molu does not infer the operational model from documentation — it reads it directly from the running system.

## Architecture: three pieces

molu is one component of a three-piece architecture. The other two are the **molu hub** and the **domain application** it fronts.

**molu (this project, the frontend).** The MCP server that speaks to the agent. Stateless. Reads the substrate's schema at startup and on change. Exposes two categories of tools to the agent: generic primitives derived from the schema (find, get, create, update, walk a state machine, run a query) and domain functions discovered from the hub.

**molu hub (specified in Part 3).** A decoupled catalogue of domain functions. The hub holds no business logic. It maintains the registry of functions published by one or more domain applications, along with each function's contract: namespaced name, description, input and output schemas, operational metadata. molu queries the hub to discover which domain functions are available and how to invoke them.

**Domain application.** The system that owns the business logic. It reads and writes to the substrate directly, and separately, it publishes its high-level functions to the hub under its own namespace. It never talks to molu; it never talks to the agent. It talks to the hub (to publish) and to the substrate (to execute).

The decoupling matters. molu does not import the domain application; the domain application does not import molu. Their only shared reference is the hub's public contract for how functions are registered and discovered. This means:

- The same molu binary works with any domain application that publishes to a hub.
- The same hub can catalogue functions from multiple domain applications.
- A domain application can be reimplemented, rewritten in a different language, or replaced entirely, and molu continues to work as long as the contract in the hub is preserved.
- New domain functions become available to the agent the moment they are published — no molu restart, no schema regeneration, no client update.

## Two axes of control

molu inherits, and depends on, two independent axes of control that together determine what an agent can do.

**The hub decides what is offered.** A domain application publishes only the functions it wishes to expose to agents. Functions not published are invisible; molu cannot invoke what the hub has not catalogued. This is the coarse-grained gate: the boundary of the agentic surface is set by the application, not by the substrate.

**The substrate decides what can execute.** Even for functions the hub offers, execution is governed by the substrate's own rules. In xolu specifically, state-machine definitions determine which transitions are valid for a given entity, tenant, and actor at a given moment. An agent may invoke a published function; the substrate may still refuse to advance the state if the transition is disallowed.

The two axes are complementary, not redundant. The hub answers "should the agent see this?"; the substrate answers "is this specific execution permitted right now?". Neither can substitute for the other. Together they produce a system where the agent's authority is precisely bounded by the domain application's public surface and the substrate's live authorisation.

## Design principles

Four principles shape the specification.

**Stateless.** molu holds no persistent state. Session context, schema cache, and semantic map are all in-memory and derived from live reads of the substrate and the hub. Restarting molu loses nothing except cache warmth.

**Contract-first.** Every boundary — molu's MCP tool surface, the hub's registration and discovery API, the substrate's REST API — is defined by an explicit contract that its consumers can rely on. Implementations may evolve; contracts change only with versioning.

**Offline-capable to the extent the substrate is.** molu adds no external dependencies beyond the substrate and the hub. If both can run in a disconnected environment, so can molu. There is no hardcoded requirement for cloud services, external LLM APIs, or third-party auth. Any feature that would introduce such a dependency is opt-in, documented as such, and degrades gracefully in its absence.

**Domain-agnostic.** molu contains no domain vocabulary. Words like "invoice", "asset", "ticket", "customer" appear in molu only as strings passed through from the schema or the hub. molu's tool descriptions, error messages, and internal logic reference entity types by their schema-declared names, not by hardcoded domain concepts. This is what makes the same molu binary reusable across unrelated applications.

## Roadmap

This document is Part 1 of three.

- **Part 2 — molu MCP frontend.** Specifies the internal architecture of molu itself: schema reader, semantic map, MCP tool surface, xolu client, MCP transport, configuration, and offline degradation policy.

- **Part 3 — molu hub.** Specifies the hub as a public component: the contract by which domain applications register functions, the discovery protocol by which consumers like molu query the catalogue, and the operational guarantees the hub provides.

---

*Draft 02. Pre-implementation. Subject to revision as the reference implementation matures.*
