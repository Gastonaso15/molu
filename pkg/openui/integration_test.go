package openui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestOpenAILLMIntegration prueba la integración completa con un modelo LLM a través de OLLAMA.
// Este test:
// 1. Genera un system prompt con un catálogo de herramientas de ejemplo
// 2. Llama a OLLAMA (qwen3.5:4b) para que genere OpenUI Lang
// 3. Parsea el resultado con nuestro parser de Go
// 4. Muestra el árbol de elementos evaluado
func TestOpenAILLMIntegration(t *testing.T) {
	// 1. Crear un catálogo de herramientas de ejemplo (similar a lo que tendría Molu)
	candidates := []ToolCandidate{
		{Name: "walk", Description: "Walk to a location", InputSchema: map[string]any{"target": "string"}, Primitive: true, Namespace: ""},
		{Name: "find", Description: "Find an entity", InputSchema: map[string]any{"entity": "string"}, Primitive: true, Namespace: ""},
		{Name: "work_orders.CreateOrder", Description: "Create a work order", InputSchema: map[string]any{"title": "string", "description": "string"}, Primitive: false, Namespace: "work_orders"},
		{Name: "inventory.ReserveStock", Description: "Reserve stock items", InputSchema: map[string]any{"item": "string", "quantity": "integer"}, Primitive: false, Namespace: "inventory"},
	}

	// 2. Generar el system prompt usando nuestra librería
	cl := MapCatalogToLibrary(candidates)
	prompt := cl.GenerateSystemPrompt()

	// 3. Llamar a OLLAMA para que genere OpenUI Lang
	ollamaURL := "http://localhost:11434/api/chat"

	// Modelo configurable (OLLAMA_MODEL); por defecto el que usó el equipo.
	model := os.Getenv("OLLAMA_MODEL")
	if model == "" {
		model = "qwen3.5:4b"
	}
	payload := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{"role": "system", "content": prompt},
			{"role": "user", "content": "Genera una interfaz de usuario para un agente que necesita acceder a herramientas de trabajo y inventario. Usa el formato OpenUI Lang."},
		},
		"stream": false,
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("Error marshalling payload: %v", err)
	}

	resp, err := http.Post(ollamaURL, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		t.Skipf("OLLAMA no está disponible (%v): prueba de integración omitida", err)
	}

	// Asegurarse de cerrar el body
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		t.Skipf("el modelo %q no está instalado en OLLAMA (ollama pull %s) u OLLAMA no lo conoce: prueba omitida", model, model)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("OLLAMA returned status code %d", resp.StatusCode)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Error decoding OLLAMA response: %v", err)
	}

	// Extract the response message
	message, ok := result["message"].(map[string]any)
	if !ok {
		t.Fatal("Formato de respuesta OLLAMA inesperado")
	}

	openuiLang := message["content"].(string)

	// 4. Parsear el resultado con nuestro parser de Go
	parserResult := Parse(openuiLang)
	if parserResult == nil {
		t.Fatal("Parse nunca debe devolver nil, aunque la respuesta del modelo no sea OpenUI Lang válido")
	}

	// Imprimir resultados usando fmt.Print (no tiene las restricciones de t.Logf)
	separator := strings.Repeat("=", 60)
	fmt.Println(separator)
	fmt.Println("SYSTEM PROMPT GENERADO (first 200 chars):", strings.TrimSpace(prompt)[:200])
	fmt.Println(separator)
	fmt.Println("RESPUESTA OPENUI LANG DE OLLAMA:")
	fmt.Println(openuiLang)
	fmt.Println(separator)
	fmt.Println("RESULTADO DEL PARSER:")
	fmt.Println("Errors:", parserResult.Errors)
	if parserResult.Root != nil {
		fmt.Println("Root type:", parserResult.Root.Type)
		if parserResult.Root.Props != nil {
			fmt.Println("Root props:", parserResult.Root.Props)
		}
		if parserResult.Root.Children != nil {
			fmt.Println("Root children count:", len(parserResult.Root.Children))
			for i, child := range parserResult.Root.Children {
				fmt.Println("  Child", i, "type=", child.Type)
				if child.Props != nil {
					fmt.Println("    props=", child.Props)
				}
			}
		}
	}
	fmt.Println(separator)
	fmt.Println("TEST COMPLETADO")
}