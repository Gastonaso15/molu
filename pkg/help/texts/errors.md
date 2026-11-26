summary: cómo leer los errores de molu y qué hacer con cada uno
# Errores

Los errores de molu traen un código y, casi siempre, el motivo. No los reintentes a ciegas: cambia la acción según lo que dicen.

- **`INVALID_TRANSITION`**: una regla del sistema impide el paso. `details.rule` dice cuál y el mensaje qué paso falta o qué cambiar. Léelo y haz primero lo que falta.
- **`CONTRACT_VIOLATION`**: algún argumento falta o es inválido; `invalid_fields` dice cuál.
- **`EMPTY`**: la consulta es válida y no tiene resultados. No es un fallo.
- **`NOT_FOUND`**: el id no existe; búscalo con una función `List*` o con `list`.
- **`NOT_OFFERED`**: esa función no está disponible para ti ahora.
- **`SUBSTRATE_UNAVAILABLE`**: la base no responde; reintenta un poco más tarde.
