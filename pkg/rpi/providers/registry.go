package providers

import (
	"fmt"

	"github.com/Gastonaso15/molu/pkg/rpi"
)

type ProviderConfig struct {
	RulesFile string // para static-context-rules
}

type ProviderRegistry struct {
	providers map[string]func(config ProviderConfig) (rpi.RPIProvider, error)
}

func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		providers: map[string]func(config ProviderConfig) (rpi.RPIProvider, error){
			"admit-all":            NewAdmitAllProvider,
			"static-context-rules": NewStaticContextRulesProvider,
			// score-test-double NO se registra aquí (solo tests)
		},
	}
}

func (r *ProviderRegistry) Create(name string, config ProviderConfig) (rpi.RPIProvider, error) {
	factory, ok := r.providers[name]
	if !ok {
		return nil, fmt.Errorf("unknown RPI provider: %s", name)
	}
	provider, err := factory(config)
	if err != nil {
		return nil, err
	}
	// Validar coherencia: si declara StageScore, debe implementar Scorer
	if provider.StagesImplemented()&rpi.StageScore != 0 {
		if _, ok := provider.(rpi.Scorer); !ok {
			return nil, fmt.Errorf("provider %s declares StageScore but does not implement Scorer", name)
		}
	}
	return provider, nil
}

func (r *ProviderRegistry) KnownProviders() []string {
	names := make([]string, 0, len(r.providers))
	for n := range r.providers {
		names = append(names, n)
	}
	return names
}