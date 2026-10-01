package exec

import "fmt"

// Códigos de error propios de molu (especificación, Parte 2 §8.5).
const (
	CodeUnavailable    = "XOLU-MOLU-FRONT-UNAVAILABLE"
	CodeStartup        = "XOLU-MOLU-FRONT-STARTUP"
	CodeTimeout        = "XOLU-MOLU-FRONT-TIMEOUT"
	CodeContract       = "XOLU-MOLU-FRONT-CONTRACT"
	CodeHubUnavailable = "XOLU-MOLU-FRONT-HUB-UNAVAILABLE"
)

// Error es un error de molu con código y detalle estructurado,
// con la forma que muestra la spec en §8.5.
type Error struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Detail  map[string]any `json:"detail,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}