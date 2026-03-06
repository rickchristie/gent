package jsruntime

import (
	"fmt"
	"strings"

	"github.com/grafana/sobek/ast"
	"github.com/grafana/sobek/file"
	"github.com/grafana/sobek/parser"
	"github.com/grafana/sobek/token"

	"github.com/rickchristie/gent/schema"
)

// SchemaLookupFn returns the schema for a tool name.
// Returns nil if no schema exists.
type SchemaLookupFn func(name string) *schema.Schema

// ToolCallSite represents a tool.call() found in JS
// source.
type ToolCallSite struct {
	ToolName  string
	Args      map[string]any
	Line      int
	Column    int
	IsDynamic bool
}

// PreValidationError represents a schema validation
// failure found during pre-validation.
type PreValidationError struct {
	Site         ToolCallSite
	ErrorMessage string
}

// FindToolCalls parses JS source and extracts all
// tool.call() and tool.parallelCall() invocations.
// Returns found call sites. Calls with dynamic args
// (variable references) have IsDynamic=true and nil Args.
func FindToolCalls(
	source string,
) ([]ToolCallSite, error) {
	program, err := parser.ParseFile(
		nil, "code.js", source, 0,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"parse error: %w", err,
		)
	}

	calls := findCallExpressions(program)
	var sites []ToolCallSite
	for _, call := range calls {
		extracted := extractToolCallSites(
			call, program.File,
		)
		sites = append(sites, extracted...)
	}
	return sites, nil
}

// PreValidate finds all tool.call() in source and
// validates literal args against schemas. Returns errors
// for ALL calls that fail validation. Skips calls with
// dynamic args.
func PreValidate(
	source string,
	schemaFn SchemaLookupFn,
) ([]PreValidationError, error) {
	if schemaFn == nil {
		return nil, nil
	}

	sites, err := FindToolCalls(source)
	if err != nil {
		return nil, err
	}

	var errs []PreValidationError
	for _, site := range sites {
		if site.IsDynamic {
			continue
		}
		sch := schemaFn(site.ToolName)
		if sch == nil {
			continue
		}
		msg := sch.FormatForLLM(
			site.ToolName, site.Args,
		)
		if msg != "" {
			errs = append(errs, PreValidationError{
				Site:         site,
				ErrorMessage: msg,
			})
		}
	}
	return errs, nil
}

// FormatPreValidationErrors formats all pre-validation
// errors into a single LLM-friendly message with code
// context for each failing call.
func FormatPreValidationErrors(
	source string,
	errors []PreValidationError,
) string {
	if len(errors) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(
		"%d schema pre-validation error(s):\n",
		len(errors),
	))

	for i, e := range errors {
		sb.WriteString(fmt.Sprintf(
			"\n--- Error %d: tool.call() at line %d ---"+
				"\n\n",
			i+1, e.Site.Line,
		))

		ctx := extractSourceContext(
			source,
			e.Site.Line,
			e.Site.Column,
			"", 0, 2,
		)
		if ctx != "" {
			sb.WriteString(ctx)
			sb.WriteString("\n")
		}

		sb.WriteString(e.ErrorMessage)
		if !strings.HasSuffix(e.ErrorMessage, "\n") {
			sb.WriteString("\n")
		}
	}

	sb.WriteString(
		"\nIMPORTANT: Use EXACT argument names " +
			"and types from the tool schema.\n" +
			"Fix ALL errors above before " +
			"re-submitting your code.\n",
	)
	return sb.String()
}

// extractToolCallSites checks if a CallExpression is
// tool.call() or tool.parallelCall() and extracts
// ToolCallSite(s) from it.
func extractToolCallSites(
	call *ast.CallExpression,
	f *file.File,
) []ToolCallSite {
	dot, ok := call.Callee.(*ast.DotExpression)
	if !ok {
		return nil
	}
	ident, ok := dot.Left.(*ast.Identifier)
	if !ok {
		return nil
	}
	if string(ident.Name) != "tool" {
		return nil
	}

	methodName := string(dot.Identifier.Name)
	switch methodName {
	case "call":
		return extractSingleCall(call, f)
	case "parallelCall":
		return extractParallelCall(call, f)
	default:
		return nil
	}
}

// extractSingleCall extracts a ToolCallSite from a
// tool.call({tool: "name", args: {...}}) expression.
func extractSingleCall(
	call *ast.CallExpression,
	f *file.File,
) []ToolCallSite {
	if len(call.ArgumentList) < 1 {
		return nil
	}
	site := extractCallArg(
		call.ArgumentList[0], f,
	)
	if site == nil {
		return nil
	}
	return []ToolCallSite{*site}
}

// extractParallelCall extracts ToolCallSites from a
// tool.parallelCall([{tool, args}, ...]) expression.
func extractParallelCall(
	call *ast.CallExpression,
	f *file.File,
) []ToolCallSite {
	if len(call.ArgumentList) < 1 {
		return nil
	}

	arrLit, ok := call.ArgumentList[0].(*ast.ArrayLiteral)
	if !ok {
		// Dynamic array - produce one dynamic site
		pos := resolvePosition(
			call.ArgumentList[0], f,
		)
		return []ToolCallSite{{
			IsDynamic: true,
			Line:      pos.Line,
			Column:    pos.Column,
		}}
	}

	var sites []ToolCallSite
	for _, elem := range arrLit.Value {
		if elem == nil {
			continue
		}
		site := extractCallArg(elem, f)
		if site != nil {
			sites = append(sites, *site)
		}
	}
	return sites
}

// extractCallArg extracts a ToolCallSite from a single
// argument expression (the {tool: "...", args: {...}}
// object).
func extractCallArg(
	expr ast.Expression,
	f *file.File,
) *ToolCallSite {
	pos := resolvePosition(expr, f)
	site := &ToolCallSite{
		Line:   pos.Line,
		Column: pos.Column,
	}

	objLit, ok := expr.(*ast.ObjectLiteral)
	if !ok {
		// Dynamic argument (variable, call, etc.)
		site.IsDynamic = true
		return site
	}

	var toolName string
	var toolNameDynamic bool
	var args map[string]any
	var argsDynamic bool

	for _, prop := range objLit.Value {
		keyed, ok := prop.(*ast.PropertyKeyed)
		if !ok {
			continue
		}
		key := propertyKeyName(keyed.Key)
		switch key {
		case "tool":
			strLit, ok := keyed.Value.(*ast.StringLiteral)
			if ok {
				toolName = string(strLit.Value)
			} else {
				toolNameDynamic = true
			}
		case "args":
			argsObj, ok := keyed.Value.(*ast.ObjectLiteral)
			if ok {
				extracted, dynamic := extractObjectLiteral(
					argsObj,
				)
				args = extracted
				argsDynamic = dynamic
			} else {
				argsDynamic = true
			}
		}
	}

	if toolNameDynamic {
		site.IsDynamic = true
		return site
	}

	site.ToolName = toolName
	if argsDynamic {
		site.IsDynamic = true
		return site
	}

	site.Args = args
	return site
}

// extractObjectLiteral converts an AST ObjectLiteral to a
// map[string]any. Returns (map, isDynamic). If any value
// is dynamic, isDynamic is true and the map is nil.
func extractObjectLiteral(
	obj *ast.ObjectLiteral,
) (map[string]any, bool) {
	result := make(map[string]any)
	for _, prop := range obj.Value {
		switch p := prop.(type) {
		case *ast.PropertyKeyed:
			key := propertyKeyName(p.Key)
			if key == "" {
				return nil, true
			}
			val, ok := extractLiteralValue(p.Value)
			if !ok {
				return nil, true
			}
			result[key] = val
		case *ast.PropertyShort:
			// Shorthand property (e.g. { x })
			// references a variable, so it's dynamic.
			return nil, true
		case *ast.SpreadElement:
			return nil, true
		default:
			return nil, true
		}
	}
	return result, false
}

// extractLiteralValue converts an AST Expression to a Go
// value if it's a literal. Returns (value, true) for
// literals, (nil, false) for dynamic expressions.
func extractLiteralValue(
	expr ast.Expression,
) (any, bool) {
	switch e := expr.(type) {
	case *ast.StringLiteral:
		return string(e.Value), true
	case *ast.NumberLiteral:
		return e.Value, true
	case *ast.BooleanLiteral:
		return e.Value, true
	case *ast.NullLiteral:
		return nil, true
	case *ast.ObjectLiteral:
		m, dynamic := extractObjectLiteral(e)
		if dynamic {
			return nil, false
		}
		return m, true
	case *ast.ArrayLiteral:
		arr := make([]any, 0, len(e.Value))
		for _, elem := range e.Value {
			if elem == nil {
				arr = append(arr, nil)
				continue
			}
			val, ok := extractLiteralValue(elem)
			if !ok {
				return nil, false
			}
			arr = append(arr, val)
		}
		return arr, true
	case *ast.UnaryExpression:
		if e.Operator == token.MINUS {
			val, ok := extractLiteralValue(e.Operand)
			if !ok {
				return nil, false
			}
			switch v := val.(type) {
			case int64:
				return -v, true
			case float64:
				return -v, true
			default:
				return nil, false
			}
		}
		return nil, false
	default:
		return nil, false
	}
}

// propertyKeyName extracts the string key name from a
// property key expression.
func propertyKeyName(expr ast.Expression) string {
	switch e := expr.(type) {
	case *ast.Identifier:
		return string(e.Name)
	case *ast.StringLiteral:
		return string(e.Value)
	default:
		return ""
	}
}

// resolvePosition converts an AST node index to a
// file.Position with line and column numbers.
func resolvePosition(
	expr ast.Expression,
	f *file.File,
) file.Position {
	if f == nil {
		return file.Position{}
	}
	idx := int(expr.Idx0()) - f.Base()
	return f.Position(idx)
}

// findCallExpressions walks the entire AST program and
// returns all CallExpression nodes found.
func findCallExpressions(
	program *ast.Program,
) []*ast.CallExpression {
	var calls []*ast.CallExpression
	for _, stmt := range program.Body {
		calls = walkStatement(stmt, calls)
	}
	return calls
}

// walkStatement recursively walks a statement and collects
// all CallExpression nodes.
func walkStatement(
	stmt ast.Statement,
	calls []*ast.CallExpression,
) []*ast.CallExpression {
	if stmt == nil {
		return calls
	}
	switch s := stmt.(type) {
	case *ast.ExpressionStatement:
		calls = walkExpression(
			s.Expression, calls,
		)
	case *ast.VariableStatement:
		for _, b := range s.List {
			calls = walkExpression(
				b.Initializer, calls,
			)
		}
	case *ast.LexicalDeclaration:
		for _, b := range s.List {
			calls = walkExpression(
				b.Initializer, calls,
			)
		}
	case *ast.BlockStatement:
		if s == nil {
			return calls
		}
		for _, inner := range s.List {
			calls = walkStatement(inner, calls)
		}
	case *ast.IfStatement:
		calls = walkExpression(s.Test, calls)
		calls = walkStatement(s.Consequent, calls)
		calls = walkStatement(s.Alternate, calls)
	case *ast.ForStatement:
		calls = walkStatement(s.Body, calls)
	case *ast.ForInStatement:
		calls = walkStatement(s.Body, calls)
	case *ast.ForOfStatement:
		calls = walkStatement(s.Body, calls)
	case *ast.WhileStatement:
		calls = walkExpression(s.Test, calls)
		calls = walkStatement(s.Body, calls)
	case *ast.DoWhileStatement:
		calls = walkExpression(s.Test, calls)
		calls = walkStatement(s.Body, calls)
	case *ast.TryStatement:
		calls = walkStatement(s.Body, calls)
		if s.Catch != nil {
			calls = walkStatement(
				s.Catch.Body, calls,
			)
		}
		calls = walkStatement(s.Finally, calls)
	case *ast.SwitchStatement:
		calls = walkExpression(
			s.Discriminant, calls,
		)
		for _, c := range s.Body {
			calls = walkExpression(c.Test, calls)
			for _, inner := range c.Consequent {
				calls = walkStatement(inner, calls)
			}
		}
	case *ast.ReturnStatement:
		calls = walkExpression(s.Argument, calls)
	case *ast.FunctionDeclaration:
		if s.Function != nil &&
			s.Function.Body != nil {
			calls = walkStatement(
				s.Function.Body, calls,
			)
		}
	case *ast.ThrowStatement:
		calls = walkExpression(s.Argument, calls)
	}
	return calls
}

// walkExpression recursively walks an expression and
// collects all CallExpression nodes.
func walkExpression(
	expr ast.Expression,
	calls []*ast.CallExpression,
) []*ast.CallExpression {
	if expr == nil {
		return calls
	}
	switch e := expr.(type) {
	case *ast.CallExpression:
		calls = append(calls, e)
		calls = walkExpression(e.Callee, calls)
		for _, arg := range e.ArgumentList {
			calls = walkExpression(arg, calls)
		}
	case *ast.DotExpression:
		calls = walkExpression(e.Left, calls)
	case *ast.ObjectLiteral:
		for _, prop := range e.Value {
			switch p := prop.(type) {
			case *ast.PropertyKeyed:
				calls = walkExpression(
					p.Key, calls,
				)
				calls = walkExpression(
					p.Value, calls,
				)
			case *ast.SpreadElement:
				calls = walkExpression(
					p.Expression, calls,
				)
			}
		}
	case *ast.ArrayLiteral:
		for _, elem := range e.Value {
			calls = walkExpression(elem, calls)
		}
	case *ast.BinaryExpression:
		calls = walkExpression(e.Left, calls)
		calls = walkExpression(e.Right, calls)
	case *ast.AssignExpression:
		calls = walkExpression(e.Left, calls)
		calls = walkExpression(e.Right, calls)
	case *ast.ConditionalExpression:
		calls = walkExpression(e.Test, calls)
		calls = walkExpression(
			e.Consequent, calls,
		)
		calls = walkExpression(
			e.Alternate, calls,
		)
	case *ast.UnaryExpression:
		calls = walkExpression(e.Operand, calls)
	case *ast.ArrowFunctionLiteral:
		if body, ok :=
			e.Body.(*ast.BlockStatement); ok {
			calls = walkStatement(body, calls)
		} else if body, ok :=
			e.Body.(*ast.ExpressionBody); ok {
			calls = walkExpression(
				body.Expression, calls,
			)
		}
	case *ast.FunctionLiteral:
		if e.Body != nil {
			calls = walkStatement(e.Body, calls)
		}
	case *ast.SequenceExpression:
		for _, inner := range e.Sequence {
			calls = walkExpression(inner, calls)
		}
	case *ast.NewExpression:
		calls = walkExpression(e.Callee, calls)
		for _, arg := range e.ArgumentList {
			calls = walkExpression(arg, calls)
		}
	case *ast.TemplateLiteral:
		for _, inner := range e.Expressions {
			calls = walkExpression(inner, calls)
		}
	}
	return calls
}
