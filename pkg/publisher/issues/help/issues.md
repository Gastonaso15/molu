summary: incidencias: crear, consultar y avanzar su ciclo de vida
# Incidencias (`issues.*`)

- **`issues.CreateIssue`**: `title`, `asset_id`, `priority` (critical, high, medium, low) y `severity` (critical, major, minor); opcionales `description` y `reporter_id`. Calcula los plazos de primera respuesta y de resolución según la criticidad del activo y la prioridad.
- **`issues.GetIssue`**: devuelve la incidencia por `issue_id`.
- **`issues.AdvanceIssue`**: avanza el ciclo de vida con `transition` (triage, start, hold, resume, resolve, close, reject), con `actor_id` opcional. El ciclo va de reportada a triada, en curso, en espera, resuelta, cerrada o rechazada; una transición que no corresponde al estado actual se rechaza con `INVALID_TRANSITION` y el motivo.

Si el usuario no dice qué activo, búscalo con `list` o `query` sobre `assets` antes de preguntar.
