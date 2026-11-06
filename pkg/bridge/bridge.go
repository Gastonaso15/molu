package bridge

import (
	"context"
	"errors"
	"time"

	"github.com/Gastonaso15/molu/pkg/hub"
	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/taxonomy"
	"github.com/Gastonaso15/molu/pkg/telemetry"
)

// ExecutionBridge coordinates admission gating, schema validation, and HTTP dispatch for domain functions.
type ExecutionBridge struct {
	lookup      ToolLookup
	rpiProvider rpi.RPIProvider
	resolver    SecretResolver
	caller      DomainCaller
	logger      telemetry.Logger
	metrics     telemetry.Metrics
}

// NewExecutionBridge constructs an ExecutionBridge with provided dependencies and sensible defaults.
func NewExecutionBridge(lookup ToolLookup, provider rpi.RPIProvider, resolver SecretResolver, caller DomainCaller) *ExecutionBridge {
	if resolver == nil {
		resolver = NewEnvSecretResolver()
	}
	if caller == nil {
		caller = NewHTTPDomainCaller(nil, resolver, 0)
	}
	return &ExecutionBridge{
		lookup:      lookup,
		rpiProvider: provider,
		resolver:    resolver,
		caller:      caller,
	}
}

// SetLogger attaches telemetry logger to the bridge and underlying caller.
func (b *ExecutionBridge) SetLogger(logger telemetry.Logger) {
	b.logger = logger
	if hc, ok := b.caller.(*HTTPDomainCaller); ok {
		hc.SetLogger(logger)
	}
}

// SetMetrics attaches telemetry metrics to the bridge and underlying caller.
func (b *ExecutionBridge) SetMetrics(metrics telemetry.Metrics) {
	b.metrics = metrics
	if hc, ok := b.caller.(*HTTPDomainCaller); ok {
		hc.SetMetrics(metrics)
	}
}

// Execute orchestrates lookup, RPI admission, schema validation, and domain invocation.
func (b *ExecutionBridge) Execute(ctx context.Context, toolName string, args map[string]any, actx rpi.AdmitContext) (any, error) {
	start := time.Now()

	// 1. Tool Lookup
	if b.lookup == nil {
		return nil, taxonomy.NewNotOffered(actx.Tenant, toolName)
	}
	reg, ok := b.lookup.Lookup(toolName)
	if !ok {
		return nil, taxonomy.NewNotOffered(actx.Tenant, toolName)
	}

	// 2. RPI Admission Gating
	if b.rpiProvider != nil {
		cand := rpi.ToolCandidate{
			Name:      reg.Name,
			Namespace: reg.Namespace,
			Primitive: reg.Primitive,
		}
		if err := rpi.Offer(ctx, b.rpiProvider, actx, cand); err != nil {
			return nil, err
		}
	}

	// 3. Schema Validation
	if err := ValidateDomainInput(reg.InputSchema, args); err != nil {
		var te *taxonomy.TypedError
		if errors.As(err, &te) {
			fields, _ := te.Details["fields"].([]string)
			return nil, taxonomy.NewContractViolation(actx.Tenant, toolName, fields)
		}
		return nil, err
	}

	// 4. Extract Endpoint and Auth
	endpoint := extractEndpoint(reg)
	if endpoint == "" {
		return nil, taxonomy.NewContractViolation(actx.Tenant, toolName, []string{"missing endpoint in tool metadata"})
	}
	auth := extractAuthMetadata(reg)

	// 5. Context enrichment
	rc := telemetry.RequestContextFrom(ctx)
	if rc.TenantID == "" || rc.Tool == "" {
		enriched := *rc
		if enriched.TenantID == "" {
			enriched.TenantID = actx.Tenant
		}
		if enriched.Tool == "" {
			enriched.Tool = toolName
		}
		ctx = telemetry.WithRequestContext(ctx, &enriched)
	}

	// 6. HTTP Passthrough Execution
	res, err := b.caller.Call(ctx, endpoint, auth, args)

	latencyMs := float64(time.Since(start).Milliseconds())
	code := taxonomy.CodeFromError(err)

	if b.metrics != nil {
		b.metrics.ObserveLatency(telemetry.ComponentMoluFront, toolName, latencyMs)
		b.metrics.IncRequests(telemetry.ComponentMoluFront, "domain_tool", toolName, code)
		if err != nil {
			b.metrics.IncErrors(telemetry.ComponentMoluFront, code)
		}
	}

	if b.logger != nil {
		ctxLog := b.logger.WithContext(ctx)
		if err != nil {
			ctxLog.Application().Error().
				Str("tool", toolName).
				Str("code", code).
				Float64("latency_ms", latencyMs).
				Msg("domain tool execution failed")
		} else {
			ctxLog.Application().Info().
				Str("tool", toolName).
				Str("code", code).
				Float64("latency_ms", latencyMs).
				Msg("domain tool execution completed")
		}
	}

	return res, err
}

func extractEndpoint(reg hub.ToolRegistration) string {
	if reg.Metadata == nil {
		return ""
	}
	if ep, ok := reg.Metadata["endpoint"].(string); ok && ep != "" {
		return ep
	}
	if url, ok := reg.Metadata["url"].(string); ok && url != "" {
		return url
	}
	return ""
}

func extractAuthMetadata(reg hub.ToolRegistration) AuthMetadata {
	if reg.Metadata == nil {
		return AuthMetadata{Mode: AuthModeNone}
	}
	if authObj, ok := reg.Metadata["auth"]; ok {
		switch a := authObj.(type) {
		case AuthMetadata:
			return a
		case *AuthMetadata:
			if a != nil {
				return *a
			}
		case map[string]any:
			var meta AuthMetadata
			if m, ok := a["mode"].(string); ok {
				meta.Mode = AuthMode(m)
			}
			if h, ok := a["header"].(string); ok {
				meta.Header = h
			}
			if tr, ok := a["token_ref"].(string); ok {
				meta.TokenRef = tr
			}
			return meta
		}
	}
	return AuthMetadata{Mode: AuthModeNone}
}

