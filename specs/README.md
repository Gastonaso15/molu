# Directorio de Especificaciones (SDD)

Este directorio contiene las especificaciones formales del proyecto **Molu** siguiendo el enfoque **Spec-Driven Development (SDD)**.

## Estructura de cada Spec
Cada funcionalidad o incremento se organiza en su propia subcarpeta:

```
specs/
└── NNN-<nombre-funcionalidad>/
    ├── spec.md      # Especificación funcional en notación EARS (QUÉ y POR QUÉ)
    ├── plan.md      # Plan técnico, paquetes Go, structs, interfaces y estrategia de tests (CÓMO)
    └── tasks.md     # Desglose en micro-tareas (<30 min) con checkboxes y criterios de aceptación
```

## Convención de Nomenclatura
- `001-taxonomy-types`
- `002-xolu-probe`
- `003-schema-reader`
- `004-semantic-map`
- `005-mcp-tools`

Para comenzar una nueva spec, utiliza el comando:
```bash
/sdd-spec 001-<nombre> <descripción o idea inicial>
```
