package orders

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Gastonaso15/molu/pkg/taxonomy"
)

const maxBodyBytes = 1 << 20

// Server exposes the Service over HTTP for the Execution Bridge. Every
// /api/v1/orders/* route requires "Authorization: Bearer <token>".
type Server struct {
	svc   *Service
	token string
}

func NewServer(svc *Service, token string) *Server { return &Server{svc: svc, token: token} }

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.Handle("/api/v1/orders/create", s.guard(http.HandlerFunc(s.create)))
	mux.Handle("/api/v1/orders/get", s.guard(http.HandlerFunc(s.get)))
	mux.Handle("/api/v1/orders/advance", s.guard(http.HandlerFunc(s.advance)))
	mux.Handle("/api/v1/orders/list", s.guard(http.HandlerFunc(s.list)))
	mux.Handle("/api/v1/orders/template", s.guard(http.HandlerFunc(s.template)))
	mux.Handle("/api/v1/orders/crews/list", s.guard(s.handle(func(c *Service, r *http.Request, dec func(any) bool) (any, error) {
		var in ListCrewsInput
		if !dec(&in) {
			return nil, errHandled
		}
		return c.ListCrews(r.Context(), in)
	})))
	mux.Handle("/api/v1/orders/crews/get", s.guard(s.handle(func(c *Service, r *http.Request, dec func(any) bool) (any, error) {
		var in GetCrewInput
		if !dec(&in) {
			return nil, errHandled
		}
		return c.GetCrew(r.Context(), in)
	})))
	mux.Handle("/api/v1/orders/outsourced/list", s.guard(s.handle(func(c *Service, r *http.Request, dec func(any) bool) (any, error) {
		var in ListOutsourcedInput
		if !dec(&in) {
			return nil, errHandled
		}
		return c.ListOutsourced(r.Context(), in)
	})))
	mux.Handle("/api/v1/orders/outsourced/get", s.guard(s.handle(func(c *Service, r *http.Request, dec func(any) bool) (any, error) {
		var in GetOutsourcedInput
		if !dec(&in) {
			return nil, errHandled
		}
		return c.GetOutsourced(r.Context(), in)
	})))
	mux.Handle("/api/v1/orders/warehouses/list", s.guard(s.handle(func(c *Service, r *http.Request, dec func(any) bool) (any, error) {
		var in ListWarehousesInput
		if !dec(&in) {
			return nil, errHandled
		}
		return c.ListWarehouses(r.Context(), in)
	})))
	mux.Handle("/api/v1/orders/warehouses/get", s.guard(s.handle(func(c *Service, r *http.Request, dec func(any) bool) (any, error) {
		var in GetWarehouseInput
		if !dec(&in) {
			return nil, errHandled
		}
		return c.GetWarehouse(r.Context(), in)
	})))
	mux.Handle("/api/v1/orders/consumables/list", s.guard(s.handle(func(c *Service, r *http.Request, dec func(any) bool) (any, error) {
		var in ListConsumablesInput
		if !dec(&in) {
			return nil, errHandled
		}
		return c.ListConsumables(r.Context(), in)
	})))
	mux.Handle("/api/v1/orders/consumables/get", s.guard(s.handle(func(c *Service, r *http.Request, dec func(any) bool) (any, error) {
		var in GetConsumableInput
		if !dec(&in) {
			return nil, errHandled
		}
		return c.GetConsumable(r.Context(), in)
	})))
	mux.Handle("/api/v1/orders/stock/low", s.guard(s.handle(func(c *Service, r *http.Request, dec func(any) bool) (any, error) {
		var in ListLowStockInput
		if !dec(&in) {
			return nil, errHandled
		}
		return c.ListLowStock(r.Context(), in)
	})))
	mux.Handle("/api/v1/orders/claim", s.guard(s.handle(func(c *Service, r *http.Request, dec func(any) bool) (any, error) {
		var in ClaimInput
		if !dec(&in) {
			return nil, errHandled
		}
		return c.ClaimOrder(r.Context(), in)
	})))
	mux.Handle("/api/v1/orders/assign", s.guard(s.handle(func(c *Service, r *http.Request, dec func(any) bool) (any, error) {
		var in AssignInput
		if !dec(&in) {
			return nil, errHandled
		}
		return c.AssignOrder(r.Context(), in)
	})))
	mux.Handle("/api/v1/orders/movements/register", s.guard(s.handle(func(c *Service, r *http.Request, dec func(any) bool) (any, error) {
		var in MovementInput
		if !dec(&in) {
			return nil, errHandled
		}
		return c.RegisterMovement(r.Context(), in)
	})))
	mux.Handle("/api/v1/orders/templates", s.guard(http.HandlerFunc(s.templates)))
	return withRequestID(mux)
}

// guard enforces POST and the bearer token (constant-time compare).
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || s.token == "" || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, http.StatusUnauthorized, map[string]any{"message": "missing or invalid bearer token"})
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"message": "method not allowed"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if !decode(w, r, &in) {
		return
	}
	order, err := s.svc.CreateOrder(r.Context(), in)
	respond(w, order, err)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	var in struct {
		OrderID string `json:"order_id"`
	}
	if !decode(w, r, &in) {
		return
	}
	order, err := s.svc.GetOrder(r.Context(), in.OrderID)
	respond(w, order, err)
}

func (s *Server) advance(w http.ResponseWriter, r *http.Request) {
	var in AdvanceInput
	if !decode(w, r, &in) {
		return
	}
	res, err := s.svc.AdvanceOrder(r.Context(), in)
	respond(w, res, err)
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	var in ListOrdersInput
	if !decode(w, r, &in) {
		return
	}
	list, err := s.svc.ListOrders(r.Context(), in)
	respond(w, list, err)
}

func (s *Server) template(w http.ResponseWriter, r *http.Request) {
	var in GetTemplateInput
	if !decode(w, r, &in) {
		return
	}
	tpl, err := s.svc.GetTemplate(r.Context(), in)
	respond(w, tpl, err)
}

func (s *Server) templates(w http.ResponseWriter, r *http.Request) {
	var in ListTemplatesInput
	if !decode(w, r, &in) {
		return
	}
	list, err := s.svc.ListTemplates(r.Context(), in)
	respond(w, list, err)
}

// errHandled tells handle that the request was already answered (bad JSON).
var errHandled = errors.New("request already answered")

// handle adapts a service call to an http.Handler: it decodes the JSON body
// into the call's input, runs it and answers through respond.
func (s *Server) handle(call func(c *Service, r *http.Request, dec func(any) bool) (any, error)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out, err := call(s.svc, r, func(dst any) bool { return decode(w, r, dst) })
		if errors.Is(err, errHandled) {
			return
		}
		respond(w, out, err)
	})
}

// respond maps service outcomes to the statuses the bridge understands:
// 404 without a XOLU- code (bridge -> NOT_FOUND), 409 with the XOLU-
// INVALID_TRANSITION envelope (passed through verbatim), 503 with
// XOLU-SUBSTRATE_UNAVAILABLE and retry metadata.
func respond(w http.ResponseWriter, ok any, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, ok)
		return
	}
	var (
		nf *NotFoundError
		te *TransitionError
		ue *UnavailableError
		ve *ValidationError
	)
	switch {
	case errors.As(err, &nf):
		writeJSON(w, http.StatusNotFound, map[string]any{"message": nf.Error()})
	case errors.As(err, &te):
		writeJSON(w, http.StatusConflict, map[string]any{"code": te.Code, "message": te.Message, "details": te.Details})
	case errors.As(err, &ue):
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"code":    "XOLU-SUBSTRATE_UNAVAILABLE",
			"message": "substrate xolu unavailable",
			"details": map[string]any{
				"lastSuccessfulContact": taxonomy.NowRFC3339(),
				"nextRetryAt":           taxonomy.NextRetryRFC3339(1, 100*time.Millisecond, 5*time.Second),
				"attemptNumber":         1,
				"maxAttempts":           3,
			},
		})
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": ve.Error(), "invalid_fields": ve.Fields})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]any{"message": err.Error()})
	}
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil && !errors.Is(err, io.EOF) { // an empty body means "no arguments"
		writeJSON(w, http.StatusBadRequest, map[string]any{"message": "invalid JSON body: " + err.Error()})
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// withRequestID echoes the correlation header so a call can be traced across
// molu, the publisher and its logs.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if id := r.Header.Get("X-Request-ID"); id != "" {
			w.Header().Set("X-Request-ID", id)
		}
		next.ServeHTTP(w, r)
	})
}
