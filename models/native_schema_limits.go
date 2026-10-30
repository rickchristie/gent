package models

import "unicode/utf8"

// Count each written definition once. Following references would count shared definitions twice
// and recurse without a bound for valid recursive OpenAI schemas.
type schemaTotals struct {
	properties, optional, unions, enums, characters int
	largeEnum, complexEnum                          bool
}

func (s *schemaTotals) add(node map[string]any) {
	if _, union := node["type"].([]any); union || node["anyOf"] != nil {
		s.unions++
	}
	if value, ok := node["const"].(string); ok {
		s.characters += utf8.RuneCountInString(value)
	}
	if values, ok := node["enum"].([]any); ok {
		s.enums += len(values)
		characters := 0
		for _, value := range values {
			switch value := value.(type) {
			case string:
				characters += utf8.RuneCountInString(value)
			case map[string]any, []any:
				s.complexEnum = true
			}
		}
		s.characters += characters
		s.largeEnum = s.largeEnum || (len(values) > 250 && characters > 15000)
	}
	required := make(map[string]bool)
	if names, ok := node["required"].([]any); ok {
		for _, name := range names {
			if name, ok := name.(string); ok {
				required[name] = true
			}
		}
	}
	for _, key := range []string{"properties", "$defs", "definitions"} {
		children, _ := node[key].(map[string]any)
		for name, child := range children {
			s.characters += utf8.RuneCountInString(name)
			if key == "properties" {
				s.properties++
				if !required[name] {
					s.optional++
				}
			}
			if child, ok := child.(map[string]any); ok {
				s.add(child)
			}
		}
	}
	for _, key := range []string{"items", "additionalProperties"} {
		if child, ok := node[key].(map[string]any); ok {
			s.add(child)
		}
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf", "prefixItems"} {
		children, _ := node[key].([]any)
		for _, child := range children {
			if child, ok := child.(map[string]any); ok {
				s.add(child)
			}
		}
	}
}

func (s schemaTotals) check(profile string) string {
	if profile == "openai" || profile == "portable" {
		switch {
		case s.properties > 5000:
			return "OpenAI supports at most 5000 schema properties"
		case s.characters > 120000:
			return "OpenAI schema names and values exceed 120000 characters"
		case s.enums > 1000:
			return "OpenAI supports at most 1000 enum values"
		case s.largeEnum:
			return "OpenAI large enum exceeds 15000 characters"
		}
	}
	if profile == "anthropic" {
		switch {
		case s.optional > 24:
			return "Anthropic supports at most 24 optional parameters across strict schemas"
		case s.unions > 16:
			return "Anthropic supports at most 16 union parameters across strict schemas"
		case s.complexEnum:
			return "Anthropic does not support complex enum values"
		}
	}
	return ""
}
