package openui

import (
	"errors"
	"fmt"
	"strconv"
)

// TokenType es el tipo de token en el parser de OpenUI Lang.
type TokenType int

const (
	// TokenIdent es un identificador (nombre de variable o componente).
	TokenIdent TokenType = iota
	// TokenAssign es el operador de asignación "=".
	TokenAssign
	// TokenStr es un string literal.
	TokenStr
	// TokenNum es un número literal.
	TokenNum
	// TokenLPar es un paréntesis izquierdo "(".
	TokenLPar
	// TokenRPar es un paréntesis derecho ")".
	TokenRPar
	// TokenLBrack es un corchete izquierdo "[".
	TokenLBrack
	// TokenRBrack es un corchete derecho "]".
	TokenRBrack
	// TokenComma es una coma ",".
	TokenComma
	// TokenDot es un punto ".".
	TokenDot
	// TokenEOF es el fin de entrada.
	TokenEOF
)

// Token es un token individual en el análisis léxico.
type Token struct {
	Type  TokenType
	Value string
}

// Tokenizer convierte un string de OpenUI Lang en una lista de tokens.
type Tokenizer struct {
	input string
	pos   int
}

// Parser analiza y evalúa programas de OpenUI Lang.
type Parser struct {
	tokens    []Token
	pos       int
	depth     int  // profundidad de recursión actual de parseExpression
	tooDeep   bool // se superó maxParseDepth
	errors    []string
	bindings  map[string]any
	functionCalls map[string]func(args any) any
}

// Tokenize analiza el texto de entrada y retorna una lista de tokens.
func (t *Tokenizer) Tokenize() []Token {
	var tokens []Token

	for t.pos < len(t.input) {
		// Saltar whitespace
		if t.input[t.pos] == ' ' || t.input[t.pos] == '\t' || t.input[t.pos] == '\n' {
			if t.input[t.pos] == '\n' {
				t.pos++
				continue
			}
			t.pos++
			continue
		}

		// Manejar comentarios de una línea (//)
		if t.pos+1 < len(t.input) && t.input[t.pos] == '/' && t.input[t.pos+1] == '/' {
			// Saltar hasta el final de línea
			for t.pos < len(t.input) && t.input[t.pos] != '\n' {
				t.pos++
			}
			continue
		}

		// Numbers (enteros y decimales)
		if t.input[t.pos] >= '0' && t.input[t.pos] <= '9' {
			start := t.pos
			for t.pos < len(t.input) && (t.input[t.pos] >= '0' && t.input[t.pos] <= '9' || t.input[t.pos] == '.') {
				t.pos++
			}
			numStr := t.input[start:t.pos]
			if _, err := strconv.ParseFloat(numStr, 64); err == nil {
				tokens = append(tokens, Token{Type: TokenNum, Value: numStr})
			}
			continue
		}

		// Strings (comillas dobles)
		if t.input[t.pos] == '"' {
			start := t.pos
			t.pos++ // consumir apertura
			for t.pos < len(t.input) && t.input[t.pos] != '"' {
				if t.input[t.pos] == '\\' && t.pos+1 < len(t.input) {
					t.pos += 2 // saltar escape
					continue
				}
				t.pos++
			}
			if t.pos < len(t.input) {
				t.pos++ // consumir cierre
			}
			tokens = append(tokens, Token{Type: TokenStr, Value: t.input[start+1 : t.pos-1]})
			continue
		}

		// Identificadores y variables $var
		if (t.input[t.pos] >= 'a' && t.input[t.pos] <= 'z') ||
			(t.input[t.pos] >= 'A' && t.input[t.pos] <= 'Z') ||
			t.input[t.pos] == '_' || t.input[t.pos] == '$' {
			start := t.pos
			// El primer carácter (letra, '_' o '$') se consume siempre: antes, un '$' inicial
			// no cumplía la condición del bucle y la posición nunca avanzaba (bucle infinito).
			t.pos++
			for t.pos < len(t.input) && ((t.input[t.pos] >= 'a' && t.input[t.pos] <= 'z') ||
				(t.input[t.pos] >= 'A' && t.input[t.pos] <= 'Z') ||
				t.input[t.pos] == '_' || isDigit(t.input[t.pos])) {
				t.pos++
			}
			ident := t.input[start:t.pos]
			tokens = append(tokens, Token{Type: TokenIdent, Value: ident})
			continue
		}

		// Operadores y símbolos de un carácter
		switch t.input[t.pos] {
		case '=':
			tokens = append(tokens, Token{Type: TokenAssign, Value: "="})
			t.pos++
		case '(':
			tokens = append(tokens, Token{Type: TokenLPar, Value: "("})
			t.pos++
		case ')':
			tokens = append(tokens, Token{Type: TokenRPar, Value: ")"})
			t.pos++
		case '[':
			tokens = append(tokens, Token{Type: TokenLBrack, Value: "["})
			t.pos++
		case ']':
			tokens = append(tokens, Token{Type: TokenRBrack, Value: "]"})
			t.pos++
		case ',':
			tokens = append(tokens, Token{Type: TokenComma, Value: ","})
			t.pos++
		case '.':
			tokens = append(tokens, Token{Type: TokenDot, Value: "."})
			t.pos++
		case '/':
			// Asegurarnos de que no sea división; en OpenUI Lang el / puede aparecer en otros contextos
			tokens = append(tokens, Token{Type: TokenIdent, Value: "/"})
			t.pos++
		default:
			// Caracter desconocido, saltar
			t.pos++
		}
	}

	// Agregar token EOF
	tokens = append(tokens, Token{Type: TokenEOF, Value: ""})
	return tokens
}

// isDigit verifica si un carácter es un dígito.
func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

// NewParser crea un nuevo parser para el texto de OpenUI Lang dado.
func NewParser(input string) *Parser {
	tkr := &Tokenizer{input: input}
	tokens := tkr.Tokenize()
	return &Parser{
		tokens: tokens,
		pos:    0,
		errors: make([]string, 0),
		bindings: make(map[string]any),
	}
}

// ParseResult es el resultado de parsear y evaluar un programa de OpenUI Lang.
type ParseResult struct {
	// Root es el nodo raíz del árbol de elementos evaluado.
	Root *ElementNode
	// Errors contiene cualquier error de parseo o evaluación.
	Errors []string
	// Meta metadatos sobre el parseo.
	Meta map[string]any
}

// ElementNode es un nodo en el árbol de elementos generado por el parser.
type ElementNode struct {
	Type    string                 `json:"type"`
	Props   map[string]any         `json:"props"`
	Children []*ElementNode         `json:"children,omitempty"`
	Partial bool                   `json:"partial"`
	State   map[string]any         `json:"state,omitempty"`
}

// Statement es una statement de asignación en OpenUI Lang: identificador = expresión.
type Statement struct {
	Identifier string
	Expression *ExpressionNode
}

// ExpressionNode representa cualquier expresión en OpenUI Lang.
type ExpressionNode struct {
	Type    string        `json:"type"`
	Value   any           `json:"value,omitempty"`
	Identifier string      // Para state references como $var
	Operator string      // Para binops: +, -, *, /
	Left    *ExpressionNode
	Right   *ExpressionNode
	// Para calls: @Run, @Set, etc.
	Call    string
	CallArgs []*ExpressionNode
}

// Límites que garantizan que Parse termina y no agota memoria ni pila con entradas
// hostiles (por ejemplo, la respuesta de un modelo que se descontrola).
const (
	maxInputBytes = 1 << 20 // 1 MiB de OpenUI Lang
	maxParseDepth = 500     // anidamiento máximo de expresiones
)

// Parse parsea y evalúa el texto de OpenUI Lang y retorna un ParseResult.
// Nunca entra en un bucle infinito ni entra en pánico: una entrada inválida o
// demasiado grande devuelve un ParseResult con Errors descriptivos.
func Parse(input string) *ParseResult {
	if len(input) > maxInputBytes {
		return &ParseResult{Errors: []string{fmt.Sprintf("entrada demasiado grande: %d bytes (máximo %d)", len(input), maxInputBytes)}}
	}
	parser := NewParser(input)
	return parser.Parse()
}

// Parse usa el parser interno para analizar el programa.
func (p *Parser) Parse() *ParseResult {
	result := &ParseResult{
		Errors: p.errors,
	}

	// Parsing de statements
	var statements []*Statement
	for p.pos < len(p.tokens) && p.tokens[p.pos].Type != TokenEOF {
		stmt := p.parseStatement()
		if stmt != nil {
			statements = append(statements, stmt)
		} else {
			// Garantiza progreso: un token que no inicia una statement se descarta.
			// Sin esto, una entrada como `Text("x")` o `(` no avanzaba nunca.
			p.pos++
		}
	}
	if p.tooDeep {
		result.Errors = append(result.Errors, fmt.Sprintf("anidamiento demasiado profundo (máximo %d niveles)", maxParseDepth))
	}

	// Evaluar statements y construir el árbol
	elemTree, evalErrs := p.evaluateStatements(statements)
	result.Root = elemTree
	result.Errors = append(result.Errors, evalErrs...)

	return result
}

// parseStatement analiza una sola statement de asignación: identificador = expresión.
func (p *Parser) parseStatement() *Statement {
	// Esperar un identificador
	if p.pos >= len(p.tokens) || p.tokens[p.pos].Type != TokenIdent {
		return nil
	}

	ident := p.tokens[p.pos].Value
	p.pos++

	// Esperar "="
	if p.pos >= len(p.tokens) || p.tokens[p.pos].Type != TokenAssign {
		// No es una statement de asignación, retroceder
		p.pos-- // rewind
		return nil
	}
	p.pos++ // consumir =

	// Parsear la expresión
	expr := p.parseExpression()

	return &Statement{
		Identifier: ident,
		Expression: expr,
	}
}

// Pratt precedence levels
const (
	precLowest = 0
	precAssign = 1
	precOr     = 2
	precAnd    = 3
	precEq     = 4
	precCmp    = 5
	precAdd    = 6
	precMul    = 7
	precPrefix = 8
)

// parseExpression usa un Pratt parser para parsear expresiones.
func (p *Parser) parseExpression(precedence ...int) *ExpressionNode {
	p.depth++
	defer func() { p.depth-- }()
	if p.depth > maxParseDepth {
		p.tooDeep = true
		p.pos = len(p.tokens) // abandona el resto de la entrada
		return nil
	}

	// Obtener el operando izquierdo usando parsePrimary
	left := p.parsePrimary()

	// Buscar operadores infix mientras tengan suficiente precedencia
	for {
		if p.pos >= len(p.tokens) {
			break
		}

		currentTok := p.tokens[p.pos]

		// Si es EOF, detenerse
		if currentTok.Type == TokenEOF {
			break
		}

		// Determinar precedencia del operador actual
		var prec int
		switch currentTok.Type {
		case TokenAssign:
			prec = precAssign
		default:
			// No es un operador que soportemos en este nivel, detenerse
			break
		}

		// Si el operador no tiene suficiente precedencia, detenerse
		if len(precedence) > 0 && prec <= precedence[0] {
			break
		}

		p.pos++ // consumir el operador

		// Parsear el operando derecho con la precedencia actual
		right := p.parseExpression(prec)

		// Construir el nodo de operación binaria
		left = &ExpressionNode{
			Type:    "BinOp",
			Operator: currentTok.Value,
			Left:    left,
			Right:   right,
		}
	}

	return left
}

// parsePrimary parsea un primary expression (número, string, identificador, paréntesis).
func (p *Parser) parsePrimary() *ExpressionNode {
	if p.pos >= len(p.tokens) {
		return nil
	}

	tok := p.tokens[p.pos]

	switch tok.Type {
	case TokenNum:
		p.pos++
		num, _ := strconv.ParseFloat(tok.Value, 64)
		return &ExpressionNode{Type: "Num", Value: num}

	case TokenStr:
		p.pos++
		return &ExpressionNode{Type: "Str", Value: tok.Value}

	case TokenIdent:
		p.pos++
		// Verificar si es una referencia a variable $var
		if tok.Value != "" && len(tok.Value) > 0 && tok.Value[0] == '$' {
			return &ExpressionNode{
				Type:      "StateRef",
				Identifier: tok.Value,
			}
		}
		// Podría ser un nombre de componente o función
		return &ExpressionNode{
			Type:      "Ref",
			Identifier: tok.Value,
		}

	case TokenLPar:
		p.pos++ // consumir "("
		expr := p.parseExpression()
		if p.pos < len(p.tokens) && p.tokens[p.pos].Type == TokenRPar {
			p.pos++ // consumir ")"
		}
		return expr

	default:
		// Caracter inesperado, avanzar y retornar nil
		p.pos++
		return nil
	}
}

// evaluateStatements evalúa todas las statements y construye el árbol de elementos.
func (p *Parser) evaluateStatements(statements []*Statement) (*ElementNode, []string) {
	var errors []string
	var root *ElementNode

	// Almacenar definiciones; la última definición de un nombre gana
	defs := make(map[string]*Statement)
	for _, stmt := range statements {
		defs[stmt.Identifier] = stmt
	}

	// Si hay una statement "root", usarla como entry point
	var entryID string
	if _, ok := defs["root"]; ok {
		entryID = "root"
	} else {
		// Buscar la primera statement que tenga identificador
		for _, stmt := range statements {
			if stmt.Identifier != "" {
				entryID = stmt.Identifier
				break
			}
		}
	}

	// Evaluar la entry point
	if entryID != "" {
		if stmt, ok := defs[entryID]; ok {
			elem, err := p.evaluateExpression(stmt.Expression)
			if err != nil {
				errors = append(errors, fmt.Sprintf("error evaluando %s: %v", entryID, err))
			} else {
				root = elem
			}
		}
	}

	// Procesar todas las statements para resolver forward references
	// y construir el árbol completo
	evaluated := make(map[string]bool)

	for _, stmt := range statements {
		if evaluated[stmt.Identifier] {
			continue
		}
		evaluated[stmt.Identifier] = true

		// Evaluar la expresión
		elem, err := p.evaluateExpression(stmt.Expression)
		if err != nil {
			errors = append(errors, fmt.Sprintf("error en %s: %v", stmt.Identifier, err))
			continue
		}

		// Si es el root o aún no tenemos root, asignarlo
		if entryID == "" || stmt.Identifier == entryID {
			if root == nil {
				root = elem
			}
		}
	}

	return root, errors
}

// evaluateExpression evalúa un ExpressionNode y retorna un ElementNode.
func (p *Parser) evaluateExpression(expr *ExpressionNode) (*ElementNode, error) {
	if expr == nil {
		return nil, errors.New("expresión nula")
	}

	switch expr.Type {
	case "Num":
		return &ElementNode{
			Type: "element",
			Props: map[string]any{
				"text": fmt.Sprintf("%v", expr.Value),
			},
		}, nil

	case "Str":
		return &ElementNode{
			Type: "element",
			Props: map[string]any{
				"text": expr.Value,
			},
		}, nil

	case "Ref":
		// Referencia a un componente o función
		if expr.Identifier != "" {
			// Buscar en bindings primero
			if val, ok := p.bindings[expr.Identifier]; ok {
				elem := p.evaluateExpressionByValue(val)
				return elem, nil
			}
			// Retornar como referencia de componente
			return &ElementNode{
				Type: "element",
				Props: map[string]any{
					"name": expr.Identifier,
				},
			}, nil
		}
		return nil, errors.New("referencia vacía")

	case "StateRef":
		// $variable reference - obtener valor del contexto
		if expr.Identifier != "" {
			if val, ok := p.bindings[expr.Identifier]; ok {
				elem := p.evaluateExpressionByValue(val)
				return elem, nil
			}
			// Variable no definida, retornar placeholder
			return &ElementNode{
				Type: "element",
				Props: map[string]any{
					"text": fmt.Sprintf("$%s (undefined)", expr.Identifier),
				},
			}, nil
		}
		return nil, errors.New("state reference vacía")

	case "BinOp":
		// Operación binaria: izq <op> der
		left, leftErr := p.evaluateExpression(expr.Left)
		if leftErr != nil {
			return nil, leftErr
		}
		right, rightErr := p.evaluateExpression(expr.Right)
		if rightErr != nil {
			return nil, rightErr
		}

		// Evaluar según el operador
		switch expr.Operator {
		case "+":
			// Concatenación de strings o suma de números
			switch l := left.Props["text"].(type) {
			case string:
				switch r := right.Props["text"].(type) {
				case string:
					return &ElementNode{
						Type: "element",
						Props: map[string]any{
							"text": l + r,
						},
					}, nil
				case float64:
					return &ElementNode{
						Type: "element",
						Props: map[string]any{
							"text": l + fmt.Sprintf("%g", r),
						},
					}, nil
				}
			case float64:
				switch r := right.Props["text"].(type) {
				case string:
					return &ElementNode{
						Type: "element",
						Props: map[string]any{
							"text": fmt.Sprintf("%g", l) + r,
						},
					}, nil
				case float64:
					val := left.Props["text"].(float64) + right.Props["text"].(float64)
					return &ElementNode{
						Type: "element",
						Props: map[string]any{
							"text": fmt.Sprintf("%g", val),
						},
					}, nil
				}
			}
		case "-":
			// Resta de números
			switch l := left.Props["text"].(type) {
			case float64:
				switch r := right.Props["text"].(type) {
				case float64:
					if r > l {
						// Result would be negative, still allow it
						return &ElementNode{
							Type: "element",
							Props: map[string]any{
								"text": fmt.Sprintf("%g", l - r),
							},
						}, nil
					}
					return &ElementNode{
						Type: "element",
						Props: map[string]any{
							"text": fmt.Sprintf("%g", l - r),
						},
					}, nil
				}
			}
		case "*":
			// Multiplicación
			switch l := left.Props["text"].(type) {
			case float64:
				switch r := right.Props["text"].(type) {
				case float64:
					val := l * r
					return &ElementNode{
						Type: "element",
						Props: map[string]any{
							"text": fmt.Sprintf("%g", val),
						},
					}, nil
				}
			}
		case "/":
			// División
			switch l := left.Props["text"].(type) {
			case float64:
				switch r := right.Props["text"].(type) {
				case float64:
					if r == 0 {
						return nil, errors.New("división por cero")
					}
					val := l / r
					return &ElementNode{
						Type: "element",
						Props: map[string]any{
							"text": fmt.Sprintf("%g", val),
						},
					}, nil
				}
			}
		}

		return left, nil

	case "StateAssign":
		// $var = expr - almacenar el valor evaluado en el bindings map
		if expr.Identifier != "" {
			// Evaluar el valor a la derecha del =
			if expr.Right != nil {
				val, err := p.evaluateExpression(expr.Right)
				if err != nil {
					return nil, err
				}
				// Almacenar en bindings quitando el prefijo $
				varName := expr.Identifier
				if len(varName) > 1 && varName[0] == '$' {
					p.bindings[varName[1:]] = val.Props["text"]
				}
			}
		}
		// Retornar el valor evaluado a la derecha
		if expr.Right != nil {
			return p.evaluateExpression(expr.Right)
		}
		return nil, nil

	default:
		// Unknown expression type, return as-is as a generic element
		return &ElementNode{
			Type: "element",
			Props: map[string]any{
				"text": fmt.Sprintf("%v", expr),
			},
		}, nil
	}
}

// evaluateExpressionByValue evalúa un elemento usando un valor conocido.
func (p *Parser) evaluateExpressionByValue(val any) *ElementNode {
	switch v := val.(type) {
	case string:
		return &ElementNode{
			Type: "element",
			Props: map[string]any{
				"text": v,
			},
		}
	case float64:
		return &ElementNode{
			Type: "element",
			Props: map[string]any{
				"text": fmt.Sprintf("%g", v),
			},
		}
	case int:
		return &ElementNode{
			Type: "element",
			Props: map[string]any{
				"text": fmt.Sprintf("%d", v),
			},
		}
	default:
		s := fmt.Sprintf("%v", val)
		return &ElementNode{
			Type: "element",
			Props: map[string]any{
				"text": s,
			},
		}
	}
}