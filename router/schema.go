package router

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidateArguments implements the high-value subset of JSON Schema needed at
// the routing boundary: required fields, primitive types, enums, and unknown
// top-level properties. MCP servers remain the final schema authority.
func ValidateArguments(tool Tool, arguments map[string]any) []ValidationError {
	var schema map[string]any
	if json.Unmarshal(tool.InputSchema, &schema) != nil {
		return []ValidationError{{Message: "tool input schema is invalid"}}
	}
	properties, _ := schema["properties"].(map[string]any)
	required := make(map[string]struct{})
	if values, ok := schema["required"].([]any); ok {
		for _, value := range values {
			if name, ok := value.(string); ok {
				required[name] = struct{}{}
			}
		}
	}
	var errors []ValidationError
	for name := range required {
		if value, exists := arguments[name]; !exists || value == nil {
			errors = appendValidation(errors, name, "is required")
		}
	}
	for name, value := range arguments {
		raw, known := properties[name]
		if !known {
			if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
				errors = appendValidation(errors, name, "is not allowed")
			}
			continue
		}
		property, _ := raw.(map[string]any)
		if expected, _ := property["type"].(string); expected != "" && !matchesType(value, expected) {
			errors = appendValidation(errors, name, "must be "+expected)
		}
		if enum, ok := property["enum"].([]any); ok && !contains(enum, value) {
			errors = appendValidation(errors, name, "is not one of the allowed values")
		}
	}
	sort.Slice(errors, func(i, j int) bool { return errors[i].Field < errors[j].Field })
	return errors
}

func appendValidation(errors []ValidationError, field, message string) []ValidationError {
	return append(errors, ValidationError{Field: field, Message: message})
}

func matchesType(value any, expected string) bool {
	switch expected {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		_, ok := value.(float64)
		if ok {
			return true
		}
		_, ok = value.(float32)
		return ok || isInteger(value)
	case "integer":
		return isInteger(value)
	case "array":
		return reflect.ValueOf(value).IsValid() && reflect.ValueOf(value).Kind() == reflect.Slice
	case "object":
		return reflect.ValueOf(value).IsValid() && reflect.ValueOf(value).Kind() == reflect.Map
	case "null":
		return value == nil
	default:
		return true
	}
}

func isInteger(value any) bool {
	switch value.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case float64:
		number := value.(float64)
		return number == float64(int64(number))
	default:
		return false
	}
}

func contains(values []any, target any) bool {
	for _, value := range values {
		if reflect.DeepEqual(value, target) || fmt.Sprint(value) == fmt.Sprint(target) {
			return true
		}
	}
	return false
}
