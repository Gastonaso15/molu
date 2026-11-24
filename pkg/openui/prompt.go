package openui

import (
	"bytes"
	"sort"
)

// PromptOptions son las opciones para generar el system prompt.
type PromptOptions struct {
	// Tools son las definiciones de herramientas a incluir en el prompt.
	Tools []ToolCandidate
	// ToolCalls indica si el modelo puede llamar a herramientas (@Run).
	ToolCalls bool
	// Bindings indica si deben incluirse bindings de $variables.
	Bindings bool
	// EditMode indica si se soporta editing incremental por el modelo.
	EditMode bool
	// Preamble es un texto inicial añadido al system prompt.
	Preamble string
}

// GeneratePrompt genera un system prompt de OpenUI Lang a partir de las herramientas
// candidatas y las opciones dadas. Este es el punto de entrada principal para integrar
// OpenUI con el backend de Molu.
func GeneratePrompt(opts PromptOptions) string {
	var buf bytes.Buffer

	// Preamble si está definido
	if opts.Preamble != "" {
		buf.WriteString(opts.Preamble)
		buf.WriteString("\n\n")
	}

	// Construir la librería de componentes desde el catálogo
	cl := MapCatalogToLibrary(opts.Tools)

	// Generar la sección de library
	promptSection := cl.GenerateSystemPrompt()
	buf.WriteString(promptSection)

	// Agregar instrucciones finales basadas en las opciones
	buf.WriteString("-- Output Format --\n")
	buf.WriteString("Responde SOLO con código OpenUI Lang, sin texto adicional, sin markdown, sin explicaciones.\n")
	buf.WriteString("Genera un programa completo con statement 'root = ...' como entry point.\n")
	buf.WriteString("Usa @Run(herramienta) para ejecutar herramientas, @Set($var, val) para establecer estado,\n")
	buf.WriteString("@Reset($var) para restablecer estado.\n\n")

	if opts.ToolCalls {
		buf.WriteString("Puedes usar @Run(nombreDeLaHerramienta) para invocar herramientas admitidas.\n")
		buf.WriteString("Las herramientas vienen definidas en la library sección arriba.\n\n")
	}

	if opts.Bindings {
		buf.WriteString("$variables are reactive: changea un $variable y la UI se actualiza automáticamente.\n")
		buf.WriteString("Usa @Set($var, valor) para setear y @Reset($var) para restablecer.\n\n")
	}

	// Instrucción final
	buf.WriteString("-- Final Instruction --\n")
	buf.WriteString("Generate the OpenUI Lang program now. Start with 'root = ' followed by a composition of components.\n")
	buf.WriteString("No includas bloques de código fenced (```), no añadas texto antes o después del programa.\n")

	return buf.String()
}

// GenerateSystemPrompt es un alias compatibable con la firma que usa lang-core.
// Genera un system prompt listo para ser usado con cualquier modelo LLM.
func GenerateSystemPrompt(opts PromptOptions) string {
	return GeneratePrompt(opts)
}

// BuildLibrarySpec construye un LibrarySpec (formato JSON schema) desde el ComponentLibrary.
// Esto permite usar las utilidades de prompt generation de lang-core si es necesario.
func BuildLibrarySpec(cl *ComponentLibrary) map[string]any {
	spec := make(map[string]any)

	// Root component
	spec["root"] = cl.Root

	// Components
	components := make([]map[string]any, len(cl.Components))
	for i, comp := range cl.Components {
		components[i] = map[string]any{
			"name":    comp.Name,
			"props":   comp.Props,
			"actions": comp.Actions,
		}
	}
	sort.Slice(components, func(i, j int) bool {
		return components[i]["name"].(string) < components[j]["name"].(string)
	})
	spec["components"] = components

	// Functions
	if len(cl.Functions) > 0 {
		functions := make(map[string]any, len(cl.Functions))
		for k, v := range cl.Functions {
			functions[k] = v
		}
		spec["functions"] = functions
	}

	// Actions
	if len(cl.Actions) > 0 {
		actions := make([]string, len(cl.Actions))
		for i, a := range cl.Actions {
			actions[i] = a
		}
		spec["actions"] = actions
	}

	return spec}