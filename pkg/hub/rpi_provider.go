package hub

import (
	"fmt"
	"os"

	"github.com/Gastonaso15/molu/pkg/rpi"
	"github.com/Gastonaso15/molu/pkg/rpi/providers"
)

// NewRPIProvider builds the RPI provider selected by configuration, so that
// switching policy never requires touching code. rulesFile and rulesJSON feed
// static-context-rules (the file is read here, once); admit-all ignores them.
func NewRPIProvider(name, rulesFile, rulesJSON string) (rpi.RPIProvider, error) {
	switch name {
	case "admit-all":
		return providers.NewAdmitAllProvider(), nil
	case "static-context-rules":
		data := []byte(rulesJSON)
		switch {
		case rulesFile != "":
			var err error
			if data, err = os.ReadFile(rulesFile); err != nil {
				return nil, fmt.Errorf("static-context-rules: reading rules file %s: %w", rulesFile, err)
			}
		case rulesJSON == "":
			return nil, fmt.Errorf("static-context-rules: no rules given (rules file or inline JSON)")
		}
		rules, err := providers.ParseStaticContextRules(data)
		if err != nil {
			return nil, err
		}
		return providers.NewStaticContextRulesProvider(rules), nil
	case "":
		return nil, fmt.Errorf("RPI provider name is empty")
	}
	return nil, fmt.Errorf("unknown RPI provider %q (options: admit-all, static-context-rules)", name)
}
