---
description: SDD · Modifica o añade requisitos: primero la spec, luego el código
agent: plan
---
Nuevo requisito o cambio para la spec specs/$1/: $ARGUMENTS

NO toques código de producción ni tests todavía. Usa la skill sdd.

1. Actualiza specs/$1/spec.md con el nuevo Requisito Funcional (o ajusta los existentes) usando notación EARS, identificando casos límite y lo que quede fuera de alcance.
2. Identifica con precisión qué partes de specs/$1/plan.md y specs/$1/tasks.md deberán modificarse a continuación.
3. Muestra el `diff` de la spec y espera la aprobación expresa antes de actualizar el plan o el código.
