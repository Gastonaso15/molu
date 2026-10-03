---
description: SDD · Entrevista y genera la spec (uso: /sdd-spec 001-nombre-funcionalidad idea inicial)
agent: build
---
NO escribas código de implementación en Go (no toques `pkg/` ni `cmd/`). Lee docs/constitution.md, AGENTS.md y MEMORY.md, y usa la skill sdd.

Carpeta de la spec: specs/$1/
Idea inicial (el texto que va después del identificador de la carpeta): $ARGUMENTS

Tu trabajo:
1. Hazme preguntas de UNA en UNA para eliminar ambigüedades (máximo 5 preguntas), considerando especialmente:
   - ¿Qué eje de control interviene? (Eje 1: Hub/RPI para qué se ofrece; Eje 2: FSMs de xolu para qué se ejecuta).
   - ¿Es un primitivo genérico (NUNCA lleva namespace, ej. `walk`) o una función de dominio (SIEMPRE lleva `namespace.Nombre`)?
   - ¿Qué códigos de la taxonomía tipada de 6 resultados aplican ante fallos, objetos inexistentes o estados vacíos?
   - ¿Qué queda explícitamente fuera de esta versión?
2. Con mis respuestas, CREA Y GUARDA DIRECTAMENTE en disco el archivo `specs/$1/spec.md` (creando la carpeta `specs/$1/` si no existe) siguiendo la plantilla de la skill sdd, con los requisitos en EARS (RF-x) y "Estado: borrador".
   IMPORTANTE: Está estrictamente prohibido pedirle al usuario que copie y pegue el texto o que cree el archivo manualmente. Debes guardarlo directamente en disco usando tus herramientas de creación de archivos.
3. Solo el QUÉ y el POR QUÉ: nada de nombres de archivos Go, structs internos ni algoritmos técnicos (eso pertenecerá a plan.md).
