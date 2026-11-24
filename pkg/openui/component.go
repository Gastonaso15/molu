package openui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Gastonaso15/molu/pkg/rpi"
)

// ToolCandidate es el tipo de molu/pkg/rpi reexportado para comodidad.
type ToolCandidate = rpi.ToolCandidate

// ComponentType es el tipo de componente OpenUI que se generará para una herramienta.
type ComponentType int

const (
	// ComponentQuery representa una herramienta de consulta (Query) que devuelve datos.
	ComponentQuery ComponentType = iota
	// ComponentMutation representa una herramienta de mutación (Mutation) que modifica datos.
	ComponentMutation
	// ComponentCard representa una herramienta que se muestra en una tarjeta.
	ComponentCard
	// ComponentTable representa una herramienta que muestra datos en tabla.
	ComponentTable
	// ComponentChart representa una herramienta que muestra un chart/gráfico.
	ComponentChart
	// ComponentGeneric representa un componente genérico para herramientas sin mapping específico.
	ComponentGeneric
)

// String devuelve el nombre legible del tipo de componente.
func (c ComponentType) String() string {
	switch c {
	case ComponentQuery:
		return "Query"
	case ComponentMutation:
		return "Mutation"
	case ComponentCard:
		return "Card"
	case ComponentTable:
		return "Table"
	case ComponentChart:
		return "Chart"
	case ComponentGeneric:
		return "Generic"
	default:
		return "Unknown"
	}
}

// ComponentDef es la definición de un componente OpenUI que mapea a una ToolCandidate.
type ComponentDef struct {
	// Name es el nombre del componente en OpenUI Lang (usa camelCase si tiene namespace).
	Name string
	// Type es el tipo categoría del componente.
	Type ComponentType
	// Props son las propiedades mapeadas desde el inputSchema de la herramienta.
	Props map[string]any
	// Actions son las acciones asociadas (@Run, @Set, @Reset).
	Actions []string
	// Description es la descripción de la herramienta para el system prompt.
	Description string
}

// ComponentLibrary es una colección de definiciones de componentes que el modelo
// puede usar para generar OpenUI Lang. Este es el equivalente Go a
// @openuidev/lang-core's createLibrary.
type ComponentLibrary struct {
	// Root es el componente entry point (ej. "root = Stack([...])").
	Root string
	// Components es la lista de definiciones de componentes disponibles.
	Components []ComponentDef
	// Functions son las funciones @-prefijadas disponibles (ej. @Count, @Sum).
	Functions map[string]any
	// Actions son las acciones disponibles (ej. @Run, @Set, @Reset).
	Actions []string
}

// NewComponentLibrary crea una nueva librería de componentes vacía con un root por defecto.
func NewComponentLibrary() *ComponentLibrary {
	return &ComponentLibrary{
		Root: "root",
	}
}

// AddComponent agrega una definición de componente a la librería.
func (cl *ComponentLibrary) AddComponent(c ComponentDef) {
	cl.Components = append(cl.Components, c)
}

// SortComponents ordena los componentes por nombre para consistencia en el output.
func (cl *ComponentLibrary) SortComponents() {
	sort.Slice(cl.Components, func(i, j int) bool {
		return cl.Components[i].Name < cl.Components[j].Name
	})
}

// GenerateSystemPrompt genera el system prompt de OpenUI Lang a partir de la librería de componentes.
// Este prompt puede ser usado como system prompt para cualquier modelo LLM para que responda
// en formato OpenUI Lang.
func (cl *ComponentLibrary) GenerateSystemPrompt() string {
	var sb strings.Builder

	sb.WriteString("You are an AI assistant that generates OpenUI Lang, a compact streaming-first language for generating user interfaces.\n")
	sb.WriteString("Generate ONE program that describes a user interface using the component library below.\n")
	sb.WriteString("Each line is an assignment: identifier = Expression\n")
	sb.WriteString("The entry point MUST be the statement named 'root'.\n\n")

	sb.WriteString("## Component Library ##\n\n")

	// Escribir funciones
	if len(cl.Functions) > 0 {
		sb.WriteString("-- Functions --\n")
		for name, def := range cl.Functions {
			sb.WriteString(fmt.Sprintf("%s(%s)\n", name, formatFunctionParams(def)))
		}
		sb.WriteString("\n")
	}

	// Escribir acciones
	if len(cl.Actions) > 0 {
		sb.WriteString("-- Actions --\n")
		for _, action := range cl.Actions {
			sb.WriteString(fmt.Sprintf("%s\n", action))
		}
		sb.WriteString("\n")
	}

	// Escribir componentes
	sb.WriteString("-- Components --\n")
	for _, comp := range cl.Components {
		sb.WriteString(fmt.Sprintf("%s: %s\n", comp.Name, comp.Description))
		sb.WriteString(fmt.Sprintf("  inputSchema: %s\n", formatSchema(comp.Props)))
		if len(comp.Actions) > 0 {
			sb.WriteString(fmt.Sprintf("  actions: %s\n", strings.Join(comp.Actions, ", ")))
		}
	}
	sb.WriteString("\n")

	// Escribir variable declarations placeholder
	sb.WriteString("-- Reactive Variables --\n")
	sb.WriteString("$title = \"\"\n")
	sb.WriteString("$priority = \"medium\"\n\n")

	// Instrucciones para el modelo
	sb.WriteString("-- Instructions --\n")
	sb.WriteString("Generate a root component that composes the available components based on the user request.\n")
	sb.WriteString("Use $variables for reactive state, @Run for tool execution, @Set/@Reset for state management.\n")
	sb.WriteString("Keep the output compact and streaming-friendly.\n\n")

	sb.WriteString("Example format:\n")
	sb.WriteString(`root = Stack([header, form, tbl])
header = CardHeader("User Tools")
form = Form("search", submitBtn, [...])
tbl = Table([Col("Title", tickets.rows.title), Col("Priority", tickets.rows.priority)])
`)
	sb.WriteString("\n-- End Library --\n")

	return sb.String()
}

// formatFunctionParams formatea los parámetros de una función a partir de un mapa.
func formatFunctionParams(def any) string {
	switch v := def.(type) {
	case map[string]any:
		var params []string
		for k, v := range v {
			params = append(params, fmt.Sprintf("%s: %v", k, v))
		}
		return strings.Join(params, ", ")
	case string:
		return v
	default:
		return fmt.Sprintf("%v", def)
	}
}

// formatSchema formatea un mapa JSON Schema a una representación de string.
func formatSchema(props map[string]any) string {
	if len(props) == 0 {
		return "{}"
	}
	var keys []string
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		v := props[k]
		parts = append(parts, fmt.Sprintf("%s: %v", k, v))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// MapToolCandidateToComponent mapea un ToolCandidate de molu a una ComponentDef de OpenUI.
// Este es el corazón del mapping entre el dominio de Molu y el lenguaje OpenUI Lang.
func MapToolCandidateToComponent(tc ToolCandidate) ComponentDef {
	// Determinar el tipo de componente basado en el nombre y características de la herramienta
	var compType ComponentType
	var name string
	var description string
	var props map[string]any

	// Usar el nombre directamente; si tiene namespace, mantener la convención
	name = tc.Name
	description = tc.Description

	// Determinar tipo basado en el nombre de la herramienta
	lowerName := strings.ToLower(tc.Name)
	isPrimitive := tc.Primitive

	// Mapeo heurístico basado en patrones de nombre
	if isPrimitive {
		// Primitivos genéricos mapean a componentes genéricos
		compType = ComponentGeneric
		// Ajustar nombre para OpenUI Lang (minúscula camelCase)
		name = toCamelCase(lowerName)
	} else if strings.HasPrefix(lowerName, "query") || strings.HasPrefix(lowerName, "list") || strings.HasPrefix(lowerName, "get") {
		// Consultas mapean a Query
		compType = ComponentQuery
		name = "Query"
	} else if strings.HasPrefix(lowerName, "create") || strings.HasPrefix(lowerName, "save") || strings.HasPrefix(lowerName, "add") || strings.HasPrefix(lowerName, "post") {
		// Mutaciones mapean a Mutation
		compType = ComponentMutation
		name = "Mutation"
	} else if strings.HasPrefix(lowerName, "chart") || strings.HasPrefix(lowerName, "graph") || strings.HasPrefix(lowerName, "dashboard") {
		// Charts mapean a Chart
		compType = ComponentChart
		name = "Chart"
	} else if strings.HasPrefix(lowerName, "table") || strings.HasPrefix(lowerName, "grid") || strings.HasPrefix(lowerName, "list") {
		// Tablas mapean a Table
		compType = ComponentTable
		name = "Table"
	} else {
		// Por defecto, componente genérico
		compType = ComponentGeneric
		name = toCamelCase(lowerName)
	}

	// Mapear el inputSchema a props
	if tc.InputSchema != nil {
		props = tc.InputSchema
	} else {
		props = make(map[string]any)
	}

	// Determinar acciones basadas en el tipo
	var actions []string
	switch compType {
	case ComponentQuery:
		actions = []string{"@Run"}
	case ComponentMutation:
		actions = []string{"@Run", "@Reset"}
	case ComponentGeneric:
		actions = []string{"@Run", "@Set", "@Reset"}
	default:
		actions = []string{"@Run"}
	}

	return ComponentDef{
		Name:      name,
		Type:      compType,
		Props:     props,
		Actions:   actions,
		Description: description,
	}
}

// toCamelCase convierte una string minúscula a camelCase.
func toCamelCase(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	// Capitalizar primera letra
	result := strings.ToUpper(s[:1]) + s[1:]
	return result
}

// MapCatalogToLibrary toma un slice de ToolCandidate y devuelve un ComponentLibrary
// con todas las herramientas mapeadas a componentes OpenUI.
func MapCatalogToLibrary(candidates []ToolCandidate) *ComponentLibrary {
	cl := NewComponentLibrary()
	cl.Root = "root"

	// Mapear cada herramienta a un componente
	for _, tc := range candidates {
		comp := MapToolCandidateToComponent(tc)
		cl.AddComponent(comp)
	}

	// Ordenar para consistencia
	cl.SortComponents()

	// Agregar algunas funciones y acciones comunes
	cl.Functions = map[string]any{
		"Count": map[string]any{"of": "rows"},
		"Sum":   map[string]any{"of": "numericField"},
		"Filter": map[string]any{"on": "field", "where": "condition"},
		"Each":  map[string]any{"over": "list"},
	}
	cl.Actions = []string{"@Run", "@Set", "@Reset", "@ToAssistant"}

	return cl
}