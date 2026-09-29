package provider

import (
	"fmt"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"
)

// Gemini's function declarations accept only an OpenAPI 3.0 subset of JSON Schema, and its
// OpenAI-compatible endpoint answers anything outside it with a bare 400 "invalid argument".
// Our own tool specs stay inside that subset, but MCP connectors ship arbitrary JSON Schema
// ($schema, $defs/$ref, oneOf, const, type arrays, nested additionalProperties, formats such as
// "uri", free-form objects with no properties). geminiSanitizeSchema rewrites a schema into the
// subset, keeping what can be kept and dropping the rest.

// geminiSchemaKeys are the Schema fields Gemini function declarations accept.
var geminiSchemaKeys = map[string]struct{}{
	"type": {}, "format": {}, "title": {}, "description": {}, "nullable": {}, "enum": {},
	"items": {}, "minItems": {}, "maxItems": {}, "properties": {}, "required": {},
	"minProperties": {}, "maxProperties": {}, "minLength": {}, "maxLength": {}, "pattern": {},
	"minimum": {}, "maximum": {}, "anyOf": {}, "propertyOrdering": {},
}

// geminiFormats lists the formats Gemini accepts per type; any other format is dropped.
var geminiFormats = map[string]map[string]struct{}{
	"string":  {"enum": {}, "date-time": {}},
	"number":  {"float": {}, "double": {}},
	"integer": {"int32": {}, "int64": {}},
}

var geminiTypes = map[string]struct{}{
	"string": {}, "number": {}, "integer": {}, "boolean": {}, "array": {}, "object": {},
}

// GeminiFunctionTool builds a Chat Completions function tool whose parameter schema has been
// rewritten into the subset Gemini accepts (see geminiSanitizeSchema).
func GeminiFunctionTool(name, description string, properties map[string]interface{}, required []string) openai.ChatCompletionToolUnionParam {
	params := shared.FunctionParameters{
		"type":                 "object",
		"additionalProperties": false,
	}
	sanitized := geminiSanitizeProperties(sanitizeClaudeSchemaProperties(properties))
	if len(sanitized) > 0 {
		params["properties"] = sanitized
		if req := geminiFilterRequired(required, sanitized); len(req) > 0 {
			params["required"] = req
		}
	}
	return openai.ChatCompletionFunctionTool(shared.FunctionDefinitionParam{
		Name:        name,
		Description: openai.String(description),
		Parameters:  params,
	})
}

func geminiSanitizeProperties(props map[string]interface{}) map[string]interface{} {
	if len(props) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(props))
	for name, raw := range props {
		schema, ok := raw.(map[string]interface{})
		if !ok {
			// A boolean schema (`true`) or anything else non-object: accept any value.
			out[name] = map[string]interface{}{}
			continue
		}
		out[name] = geminiSanitizeSchema(schema)
	}
	return out
}

// geminiSanitizeSchema rewrites one JSON Schema node into Gemini's subset.
func geminiSanitizeSchema(in map[string]interface{}) map[string]interface{} {
	out := map[string]interface{}{}

	// oneOf is anyOf for Gemini's purposes; allOf of a single schema is that schema.
	if alts, ok := in["oneOf"].([]interface{}); ok && in["anyOf"] == nil {
		in = cloneWith(in, "anyOf", alts)
	}
	if all, ok := in["allOf"].([]interface{}); ok && len(all) == 1 {
		if only, ok := all[0].(map[string]interface{}); ok {
			merged := cloneWith(only, "", nil)
			for k, v := range in {
				if k != "allOf" {
					merged[k] = v
				}
			}
			in = merged
		}
	}

	typ, nullable := geminiType(in["type"])
	if typ != "" {
		out["type"] = typ
	}
	if nullable {
		out["nullable"] = true
	}
	if c, ok := in["const"]; ok && in["enum"] == nil {
		in = cloneWith(in, "enum", []interface{}{c})
	}

	for k, v := range in {
		if _, ok := geminiSchemaKeys[k]; !ok || k == "type" {
			continue
		}
		switch k {
		case "properties":
			if props, ok := v.(map[string]interface{}); ok {
				if p := geminiSanitizeProperties(props); len(p) > 0 {
					out["properties"] = p
				}
			}
		case "items":
			if items, ok := v.(map[string]interface{}); ok {
				out["items"] = geminiSanitizeSchema(items)
			}
		case "anyOf":
			alts, ok := v.([]interface{})
			if !ok {
				continue
			}
			var kept []interface{}
			for _, alt := range alts {
				m, ok := alt.(map[string]interface{})
				if !ok {
					continue
				}
				// {"type":"null"} alternatives become nullable on the parent.
				if t, _ := m["type"].(string); t == "null" {
					out["nullable"] = true
					continue
				}
				kept = append(kept, geminiSanitizeSchema(m))
			}
			switch len(kept) {
			case 0:
			case 1:
				for kk, vv := range kept[0].(map[string]interface{}) {
					if _, exists := out[kk]; !exists {
						out[kk] = vv
					}
				}
			default:
				out["anyOf"] = kept
			}
		case "enum":
			if vals, ok := geminiStringEnum(v); ok {
				out["enum"] = vals
				if _, typed := out["type"]; !typed {
					out["type"] = "string"
				}
			}
		case "format":
			// Checked after the loop, once the final type is known.
		default:
			out[k] = v
		}
	}

	if f, ok := in["format"].(string); ok {
		t, _ := out["type"].(string)
		if _, allowed := geminiFormats[t][f]; allowed {
			out["format"] = f
		}
	}
	// Gemini enums must be strings on a string schema.
	if _, ok := out["enum"]; ok && out["type"] != "string" {
		delete(out, "enum")
	}
	if t, _ := out["type"].(string); t == "object" {
		props, _ := out["properties"].(map[string]interface{})
		if len(props) == 0 {
			// Gemini rejects OBJECT schemas with no properties ("should be non-empty for OBJECT
			// type"). A free-form object becomes an untyped schema that accepts any value.
			delete(out, "type")
			delete(out, "properties")
			delete(out, "required")
			delete(out, "minProperties")
			delete(out, "maxProperties")
		} else if req, ok := out["required"]; ok {
			if filtered := geminiFilterRequired(toStringSlice(req), props); len(filtered) > 0 {
				out["required"] = filtered
			} else {
				delete(out, "required")
			}
		}
	} else {
		delete(out, "properties")
		delete(out, "required")
	}
	if t, _ := out["type"].(string); t != "array" {
		delete(out, "items")
		delete(out, "minItems")
		delete(out, "maxItems")
	} else if _, ok := out["items"]; !ok {
		// Gemini requires items on ARRAY schemas.
		out["items"] = map[string]interface{}{"type": "string"}
	}
	return out
}

// geminiType maps a JSON Schema "type" (string or array) to one Gemini type plus nullability.
func geminiType(v interface{}) (string, bool) {
	switch t := v.(type) {
	case string:
		if t == "null" {
			return "", true
		}
		if _, ok := geminiTypes[t]; ok {
			return t, false
		}
	case []interface{}:
		typ, nullable := "", false
		for _, e := range t {
			s, _ := e.(string)
			if s == "null" {
				nullable = true
				continue
			}
			if _, ok := geminiTypes[s]; ok && typ == "" {
				typ = s
			}
		}
		return typ, nullable
	case []string:
		return geminiType(toInterfaceSlice(t))
	}
	return "", false
}

func geminiStringEnum(v interface{}) ([]interface{}, bool) {
	var vals []interface{}
	switch e := v.(type) {
	case []interface{}:
		vals = e
	case []string:
		vals = toInterfaceSlice(e)
	default:
		return nil, false
	}
	out := make([]interface{}, 0, len(vals))
	for _, val := range vals {
		switch s := val.(type) {
		case string:
			out = append(out, s)
		case nil:
		default:
			out = append(out, fmt.Sprint(s))
		}
	}
	return out, len(out) > 0
}

// geminiFilterRequired keeps only required names that exist in props (Gemini 400s on a
// required property that is not declared).
func geminiFilterRequired(required []string, props map[string]interface{}) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, r := range required {
		if _, ok := props[r]; !ok {
			continue
		}
		if _, dup := seen[r]; dup {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, r)
	}
	return out
}

func toStringSlice(v interface{}) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []interface{}:
		out := make([]string, 0, len(s))
		for _, e := range s {
			if str, ok := e.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

func toInterfaceSlice(s []string) []interface{} {
	out := make([]interface{}, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// cloneWith returns a shallow copy of m with key set to v (no key set when key is "").
func cloneWith(m map[string]interface{}, key string, v interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(m)+1)
	for k, val := range m {
		out[k] = val
	}
	if key != "" {
		out[key] = v
	}
	return out
}
