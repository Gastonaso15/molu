---
description: SDD · Revisa la spec como un QA (solo detecta problemas, no resuelve)
agent: plan
---
Revisa specs/$1/spec.md como si fueras un QA e ingeniero de sistemas experto. Usa la skill sdd.
Lista:
1. Ambigüedades restantes (requisitos funcionales que no se pueden verificar objetivamente con tests).
2. Contradicciones entre requisitos.
3. Violaciones a los 6 códigos tipados de la taxonomía (§2.4: e.g. reportar un error genérico o confundir `NOT_FOUND` con un resultado vacío o `NOT_OFFERED`).
4. Inconsistencias en nombres de herramientas (colisión de primitivos genéricos o ausencia de `namespace.Nombre` en funciones de dominio).
5. Casos límite no cubiertos (red, timeouts de xolu, schemas vacíos o mutados, concurrencia).
6. Conflictos potenciales con docs/constitution.md.

No propongas soluciones de código todavía: solo detecta y enumera los puntos para clarificar con el usuario. Formato: lista numerada.
