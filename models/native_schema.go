package models

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/rickchristie/gent"
)

// Schema checks describe the actual provider grammar. They never remove a validation rule or
// replace an optional property with a required nullable property. Unsupported schemas stay local.
type schemaEnforcement struct {
	Native  bool   `json:"native"`
	Profile string `json:"profile"`
	Reason  string `json:"reason,omitempty"`
}

func nativeSchema(profile string, schema map[string]any) schemaEnforcement {
	decision := schemaEnforcement{Profile: profile}
	if schema == nil {
		decision.Reason = "no output schema requested"
		return decision
	}
	if profile == "" {
		decision.Reason = "endpoint does not support native answer schemas"
		return decision
	}
	switch profile {
	case "openai", "anthropic", "gemini", "xai", "xgrammar", "llamacpp", "portable":
	default:
		decision.Reason = "unknown native schema profile"
		return decision
	}
	// Use one JSON representation. Schema builders can supply []string or []any and integer or
	// floating-point bounds; these Go representation details do not change the native contract.
	data, err := json.Marshal(schema)
	if err != nil {
		decision.Reason = err.Error()
		return decision
	}
	var root map[string]any
	if err = json.Unmarshal(data, &root); err != nil {
		decision.Reason = err.Error()
		return decision
	}
	if (profile == "openai" || profile == "portable") &&
		(root["type"] != "object" || root["anyOf"] != nil) {
		decision.Reason = "OpenAI requires an object root without anyOf"
		return decision
	}
	totals := schemaTotals{}
	totals.add(root)
	decision.Reason = totals.check(profile)
	if decision.Reason == "" {
		decision.Reason = checkNativeSchema(profile, root, root, nil, 0, false)
	}
	decision.Native = decision.Reason == ""
	return decision
}

func checkNativeSchema(profile string, node, root map[string]any,
	refs map[string]bool, depth int, inAllOf bool,
) string {
	if depth > 64 || (profile == "openai" && depth > 10) {
		return "schema exceeds the supported nesting depth"
	}
	if profile == "anthropic" && node["$ref"] != nil && (inAllOf || node["allOf"] != nil) {
		return "Anthropic does not support allOf with references"
	}
	if profile == "llamacpp" {
		if node["properties"] != nil && node["anyOf"] != nil {
			return "llama.cpp cannot combine properties and anyOf"
		}
		if node["additionalProperties"] == true {
			return "llama.cpp cannot guarantee valid arbitrary property names"
		}
		for _, bound := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum"} {
			if node[bound] != nil && node["type"] != "integer" {
				return "llama.cpp only enforces numeric bounds for integer types"
			}
		}
		if node["$ref"] != nil && len(refs) > 0 {
			return "llama.cpp does not support nested references"
		}
	}
	keys := make([]string, 0, len(node))
	for key := range node {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := node[key]
		if reason := nativeKeyword(profile, key, value); reason != "" {
			return reason
		}
		switch key {
		case "properties", "$defs", "definitions":
			children, ok := value.(map[string]any)
			if !ok {
				return "invalid schema object"
			}
			for _, name := range sortedMapKeys(children) {
				child, ok := children[name].(map[string]any)
				if !ok {
					return "native schemas require object property definitions"
				}
				if reason := checkNativeSchema(profile, child, root, refs, depth+1, inAllOf); reason != "" {
					return reason
				}
			}
		case "items", "additionalProperties":
			if child, ok := value.(map[string]any); ok {
				if reason := checkNativeSchema(profile, child, root, refs, depth+1, inAllOf); reason != "" {
					return reason
				}
			}
		case "anyOf", "oneOf", "allOf", "prefixItems":
			children, ok := value.([]any)
			if !ok || len(children) == 0 {
				return "invalid schema alternatives"
			}
			for _, child := range children {
				object, ok := child.(map[string]any)
				if !ok {
					return "native schemas require object alternatives"
				}
				reason := checkNativeSchema(profile, object, root, refs, depth+1, inAllOf || key == "allOf")
				if reason != "" {
					return reason
				}
			}
		case "$ref":
			ref, ok := value.(string)
			if !ok || !strings.HasPrefix(ref, "#") {
				return "native schemas do not support external references"
			}
			if refs[ref] {
				if profile == "openai" || profile == "xgrammar" {
					continue
				}
				return "native schema profile does not support recursive references"
			}
			target := schemaReference(root, ref)
			if target == nil {
				return "unresolved schema reference"
			}
			if refs == nil {
				refs = make(map[string]bool)
			}
			refs[ref] = true
			reason := checkNativeSchema(profile, target, root, refs, depth+1, inAllOf)
			delete(refs, ref)
			if reason != "" {
				return reason
			}
		}
	}
	if node["type"] == "object" || node["properties"] != nil {
		if profile == "openai" || profile == "anthropic" || profile == "portable" {
			if node["additionalProperties"] != false {
				return "native schema requires additionalProperties false"
			}
		}
		if profile == "openai" || profile == "portable" {
			properties, _ := node["properties"].(map[string]any)
			required, _ := node["required"].([]any)
			names := make(map[string]bool)
			for _, name := range required {
				if text, ok := name.(string); ok {
					names[text] = true
				}
			}
			allRequired := len(properties) == len(names)
			for name := range properties {
				allRequired = allRequired && names[name]
			}
			if !allRequired {
				return "OpenAI requires every property to be required"
			}
		}
		if (profile == "xai" || profile == "llamacpp") && node["additionalProperties"] == nil {
			return "native object defaults differ from JSON Schema"
		}
	}
	return ""
}

func nativeKeyword(profile, key string, value any) string {
	common := "|type|properties|required|additionalProperties|items|enum|const|anyOf|" +
		"$ref|$defs|definitions|title|description|default|$schema|"
	if strings.Contains(common, "|"+key+"|") {
		return ""
	}
	allowed := ""
	switch profile {
	case "openai":
		allowed = "|minimum|maximum|exclusiveMinimum|exclusiveMaximum|multipleOf|" +
			"minItems|maxItems|pattern|format|"
	case "anthropic":
		allowed = "|allOf|pattern|format|minItems|"
		if key == "minItems" && value != float64(0) && value != float64(1) {
			return "Anthropic only enforces minItems 0 or 1"
		}
	case "gemini":
		allowed = "|minimum|maximum|minItems|maxItems|prefixItems|format|"
	case "xai":
		allowed = "|minimum|maximum|exclusiveMinimum|exclusiveMaximum|minLength|maxLength|" +
			"minItems|maxItems|minProperties|maxProperties|format|allOf|"
		limits := map[string]float64{"minLength": 2048, "maxLength": 2048,
			"minItems": 256, "maxItems": 256, "minProperties": 64, "maxProperties": 64}
		if bound, ok := limits[key]; ok {
			if n, ok := value.(float64); !ok || n > bound {
				return "xAI cannot enforce this constraint bound"
			}
		}
		if key == "allOf" {
			if alternatives, ok := value.([]any); !ok || len(alternatives) != 1 {
				return "xAI only enforces allOf with one alternative"
			}
		}
	case "xgrammar":
		allowed = "|minimum|maximum|exclusiveMinimum|exclusiveMaximum|minLength|maxLength|" +
			"minItems|maxItems|pattern|format|prefixItems|"
	case "llamacpp":
		allowed = "|minimum|maximum|exclusiveMinimum|exclusiveMaximum|minLength|maxLength|" +
			"minItems|maxItems|pattern|format|"
	case "portable":
	default:
		return "unknown native schema profile"
	}
	if !strings.Contains(allowed, "|"+key+"|") {
		return fmt.Sprintf("%s does not enforce schema keyword %s", profile, key)
	}
	if key == "format" {
		format, _ := value.(string)
		formats := "|date-time|time|date|email|hostname|ipv4|ipv6|uuid|"
		if profile == "anthropic" {
			formats += "uri|duration|"
		}
		if profile == "xai" {
			formats = "|date|time|date-time|email|uuid|ipv4|ipv6|uri|"
		}
		if profile == "llamacpp" || profile == "xgrammar" {
			formats = "|date|time|date-time|uuid|"
		}
		if profile == "gemini" {
			formats = "|date|time|date-time|"
		}
		if !strings.Contains(formats, "|"+format+"|") {
			return "unsupported native string format"
		}
	}
	if key == "pattern" {
		pattern, ok := value.(string)
		if !ok {
			return "invalid native pattern"
		}
		if profile == "llamacpp" &&
			(!strings.HasPrefix(pattern, "^") || !strings.HasSuffix(pattern, "$")) {
			return "llama.cpp requires an anchored pattern"
		}
		for _, syntax := range []string{`\1`, `\2`, `\k`, `\b`, `\B`, "(?=", "(?!", "(?<"} {
			if strings.Contains(pattern, syntax) {
				return "native pattern syntax is unsupported"
			}
		}
	}
	return ""
}

func sortedMapKeys(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func schemaReference(root map[string]any, reference string) map[string]any {
	if reference == "#" {
		return root
	}
	if !strings.HasPrefix(reference, "#/") {
		return nil
	}
	var value any = root
	for _, key := range strings.Split(reference[2:], "/") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		key = strings.ReplaceAll(strings.ReplaceAll(key, "~1", "/"), "~0", "~")
		value = object[key]
	}
	object, _ := value.(map[string]any)
	return object
}

func recordSchema(result *gent.ContentResponse, decision schemaEnforcement) {
	if result == nil {
		return
	}
	if result.Info.RawGenerationInfo == nil {
		result.Info.RawGenerationInfo = make(map[string]any)
	}
	result.Info.RawGenerationInfo["output_schema"] = map[string]any{
		"native": decision.Native, "profile": decision.Profile, "reason": decision.Reason,
	}
}
