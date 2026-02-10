// Package format provides conversion between Anthropic and Google Generative AI formats.
// This file corresponds to src/format/schema-sanitizer.js in the Node.js version.
package format

import (
	"fmt"
	"strings"
)

// SanitizeSchema sanitizes JSON Schema for Antigravity API compatibility.
// Uses allowlist approach - only permit known-safe JSON Schema features.
// Converts "const" to equivalent "enum" for compatibility.
// Generates placeholder schema for empty tool schemas.
func SanitizeSchema(schema map[string]interface{}) map[string]interface{} {
	if schema == nil || len(schema) == 0 {
		// Empty/missing schema - generate placeholder with reason property
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Reason for calling this tool",
				},
			},
			"required": []string{"reason"},
		}
	}

	// Allowlist of permitted JSON Schema fields
	allowedFields := map[string]bool{
		"type":        true,
		"description": true,
		"properties":  true,
		"required":    true,
		"items":       true,
		"enum":        true,
		"title":       true,
	}

	sanitized := make(map[string]interface{})

	for key, value := range schema {
		// Convert "const" to "enum" for compatibility
		if key == "const" {
			sanitized["enum"] = []interface{}{value}
			continue
		}

		// Skip fields not in allowlist
		if !allowedFields[key] {
			continue
		}

		if key == "properties" {
			if props, ok := value.(map[string]interface{}); ok {
				newProps := make(map[string]interface{})
				for propKey, propValue := range props {
					if propMap, ok := propValue.(map[string]interface{}); ok {
						newProps[propKey] = SanitizeSchema(propMap)
					} else {
						newProps[propKey] = propValue
					}
				}
				sanitized["properties"] = newProps
			}
		} else if key == "items" {
			if itemsMap, ok := value.(map[string]interface{}); ok {
				sanitized["items"] = SanitizeSchema(itemsMap)
			} else if itemsArr, ok := value.([]interface{}); ok {
				newItems := make([]interface{}, 0, len(itemsArr))
				for _, item := range itemsArr {
					if itemMap, ok := item.(map[string]interface{}); ok {
						newItems = append(newItems, SanitizeSchema(itemMap))
					} else {
						newItems = append(newItems, item)
					}
				}
				sanitized["items"] = newItems
			} else {
				sanitized["items"] = value
			}
		} else if valueMap, ok := value.(map[string]interface{}); ok {
			sanitized[key] = SanitizeSchema(valueMap)
		} else {
			sanitized[key] = value
		}
	}

	// Ensure we have at least a type
	if _, ok := sanitized["type"]; !ok {
		sanitized["type"] = "object"
	}

	// If object type with no properties, add placeholder
	schemaType, _ := sanitized["type"].(string)
	if schemaType == "object" {
		props, hasProps := sanitized["properties"].(map[string]interface{})
		if !hasProps || len(props) == 0 {
			sanitized["properties"] = map[string]interface{}{
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Reason for calling this tool",
				},
			}
			sanitized["required"] = []string{"reason"}
		}
	}

	return sanitized
}

// CleanSchema cleans JSON schema for Gemini API compatibility.
// Uses a multi-phase pipeline matching opencode-antigravity-auth approach.
func CleanSchema(schema map[string]interface{}) map[string]interface{} {
	if schema == nil {
		return schema
	}

	// Make a copy to avoid mutating input
	result := copyMap(schema)

	// Phase 1: Convert $refs to hints
	result = convertRefsToHints(result)

	// Phase 1b: Add enum hints (preserves enum info in description)
	result = addEnumHints(result)

	// Phase 1c: Add additionalProperties hints
	result = addAdditionalPropertiesHints(result)

	// Phase 1d: Move constraints to description (before they get stripped)
	result = moveConstraintsToDescription(result)

	// Phase 2a: Merge allOf schemas
	result = mergeAllOf(result)

	// Phase 2b: Flatten anyOf/oneOf
	result = flattenAnyOfOneOf(result)

	// Phase 2c: Flatten type arrays and update required for nullable
	result = flattenTypeArrays(result, nil, "")

	// Phase 3: Remove unsupported keywords
	unsupported := []string{
		"additionalProperties", "default", "$schema", "$defs",
		"definitions", "$ref", "$id", "$comment", "title",
		"minLength", "maxLength", "pattern", "format",
		"minItems", "maxItems", "examples", "allOf", "anyOf", "oneOf",
	}
	for _, key := range unsupported {
		delete(result, key)
	}

	// Check for unsupported 'format' in string types
	if schemaType, ok := result["type"].(string); ok && schemaType == "string" {
		if format, ok := result["format"].(string); ok {
			allowed := map[string]bool{"enum": true, "date-time": true}
			if !allowed[format] {
				delete(result, "format")
			}
		}
	}

	// Phase 4: Final cleanup - recursively clean nested schemas and validate required
	if props, ok := result["properties"].(map[string]interface{}); ok {
		newProps := make(map[string]interface{})
		for key, value := range props {
			if valueMap, ok := value.(map[string]interface{}); ok {
				newProps[key] = CleanSchema(valueMap)
			} else {
				newProps[key] = value
			}
		}
		result["properties"] = newProps
	}

	if items, ok := result["items"].(map[string]interface{}); ok {
		result["items"] = CleanSchema(items)
	} else if itemsArr, ok := result["items"].([]interface{}); ok {
		newItems := make([]interface{}, 0, len(itemsArr))
		for _, item := range itemsArr {
			if itemMap, ok := item.(map[string]interface{}); ok {
				newItems = append(newItems, CleanSchema(itemMap))
			} else {
				newItems = append(newItems, item)
			}
		}
		result["items"] = newItems
	}

	// Validate that required array only contains properties that exist
	if required, ok := result["required"].([]interface{}); ok {
		if props, ok := result["properties"].(map[string]interface{}); ok {
			definedProps := make(map[string]bool)
			for key := range props {
				definedProps[key] = true
			}
			newRequired := make([]interface{}, 0)
			for _, prop := range required {
				if propStr, ok := prop.(string); ok {
					if definedProps[propStr] {
						newRequired = append(newRequired, propStr)
					}
				}
			}
			if len(newRequired) == 0 {
				delete(result, "required")
			} else {
				result["required"] = newRequired
			}
		}
	}

	// Phase 5: Convert type to Google's uppercase format (STRING, OBJECT, ARRAY, etc.)
	if schemaType, ok := result["type"].(string); ok {
		result["type"] = toGoogleType(schemaType)
	}

	return result
}

// appendDescriptionHint appends a hint to a schema's description field
func appendDescriptionHint(schema map[string]interface{}, hint string) map[string]interface{} {
	if schema == nil {
		return schema
	}
	result := copyMap(schema)
	if desc, ok := result["description"].(string); ok && desc != "" {
		result["description"] = fmt.Sprintf("%s (%s)", desc, hint)
	} else {
		result["description"] = hint
	}
	return result
}

// scoreSchemaOption scores a schema option for anyOf/oneOf selection
func scoreSchemaOption(schema map[string]interface{}) int {
	if schema == nil {
		return 0
	}

	// Score 3: Object types with properties (most informative)
	if schema["type"] == "object" || schema["properties"] != nil {
		return 3
	}

	// Score 2: Array types with items
	if schema["type"] == "array" || schema["items"] != nil {
		return 2
	}

	// Score 1: Any other non-null type
	if schemaType, ok := schema["type"].(string); ok && schemaType != "null" {
		return 1
	}

	// Score 0: Null or no type
	return 0
}

// convertRefsToHints converts $ref references to description hints
func convertRefsToHints(schema map[string]interface{}) map[string]interface{} {
	if schema == nil {
		return schema
	}

	result := copyMap(schema)

	// Handle $ref at this level
	if ref, ok := result["$ref"].(string); ok {
		parts := strings.Split(ref, "/")
		defName := parts[len(parts)-1]
		if defName == "" {
			defName = "unknown"
		}
		hint := fmt.Sprintf("See: %s", defName)

		description := ""
		if desc, ok := result["description"].(string); ok && desc != "" {
			description = fmt.Sprintf("%s (%s)", desc, hint)
		} else {
			description = hint
		}

		return map[string]interface{}{
			"type":        "object",
			"description": description,
		}
	}

	// Recursively process properties
	if props, ok := result["properties"].(map[string]interface{}); ok {
		newProps := make(map[string]interface{})
		for key, value := range props {
			if valueMap, ok := value.(map[string]interface{}); ok {
				newProps[key] = convertRefsToHints(valueMap)
			} else {
				newProps[key] = value
			}
		}
		result["properties"] = newProps
	}

	// Recursively process items
	if items, ok := result["items"].(map[string]interface{}); ok {
		result["items"] = convertRefsToHints(items)
	} else if itemsArr, ok := result["items"].([]interface{}); ok {
		newItems := make([]interface{}, 0, len(itemsArr))
		for _, item := range itemsArr {
			if itemMap, ok := item.(map[string]interface{}); ok {
				newItems = append(newItems, convertRefsToHints(itemMap))
			} else {
				newItems = append(newItems, item)
			}
		}
		result["items"] = newItems
	}

	// Recursively process anyOf/oneOf/allOf
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		if arr, ok := result[key].([]interface{}); ok {
			newArr := make([]interface{}, 0, len(arr))
			for _, item := range arr {
				if itemMap, ok := item.(map[string]interface{}); ok {
					newArr = append(newArr, convertRefsToHints(itemMap))
				} else {
					newArr = append(newArr, item)
				}
			}
			result[key] = newArr
		}
	}

	return result
}

// mergeAllOf merges all schemas in an allOf array into a single schema
func mergeAllOf(schema map[string]interface{}) map[string]interface{} {
	if schema == nil {
		return schema
	}

	result := copyMap(schema)

	// Process allOf if present
	if allOfArr, ok := result["allOf"].([]interface{}); ok && len(allOfArr) > 0 {
		mergedProperties := make(map[string]interface{})
		mergedRequired := make(map[string]bool)
		otherFields := make(map[string]interface{})

		for _, subSchema := range allOfArr {
			subMap, ok := subSchema.(map[string]interface{})
			if !ok {
				continue
			}

			// Merge properties (later overrides earlier)
			if props, ok := subMap["properties"].(map[string]interface{}); ok {
				for key, value := range props {
					mergedProperties[key] = value
				}
			}

			// Union required arrays
			if required, ok := subMap["required"].([]interface{}); ok {
				for _, req := range required {
					if reqStr, ok := req.(string); ok {
						mergedRequired[reqStr] = true
					}
				}
			}

			// Copy other fields (first occurrence wins)
			for key, value := range subMap {
				if key != "properties" && key != "required" {
					if _, exists := otherFields[key]; !exists {
						otherFields[key] = value
					}
				}
			}
		}

		// Remove allOf
		delete(result, "allOf")

		// Merge other fields first (parent takes precedence)
		for key, value := range otherFields {
			if _, exists := result[key]; !exists {
				result[key] = value
			}
		}

		// Merge properties
		if len(mergedProperties) > 0 {
			existingProps, _ := result["properties"].(map[string]interface{})
			if existingProps == nil {
				existingProps = make(map[string]interface{})
			}
			for key, value := range mergedProperties {
				if _, exists := existingProps[key]; !exists {
					existingProps[key] = value
				}
			}
			result["properties"] = existingProps
		}

		// Merge required
		if len(mergedRequired) > 0 {
			existingRequired := make(map[string]bool)
			if req, ok := result["required"].([]interface{}); ok {
				for _, r := range req {
					if rStr, ok := r.(string); ok {
						existingRequired[rStr] = true
					}
				}
			}
			for key := range mergedRequired {
				existingRequired[key] = true
			}
			newRequired := make([]interface{}, 0, len(existingRequired))
			for key := range existingRequired {
				newRequired = append(newRequired, key)
			}
			result["required"] = newRequired
		}
	}

	// Recursively process properties
	if props, ok := result["properties"].(map[string]interface{}); ok {
		newProps := make(map[string]interface{})
		for key, value := range props {
			if valueMap, ok := value.(map[string]interface{}); ok {
				newProps[key] = mergeAllOf(valueMap)
			} else {
				newProps[key] = value
			}
		}
		result["properties"] = newProps
	}

	// Recursively process items
	if items, ok := result["items"].(map[string]interface{}); ok {
		result["items"] = mergeAllOf(items)
	} else if itemsArr, ok := result["items"].([]interface{}); ok {
		newItems := make([]interface{}, 0, len(itemsArr))
		for _, item := range itemsArr {
			if itemMap, ok := item.(map[string]interface{}); ok {
				newItems = append(newItems, mergeAllOf(itemMap))
			} else {
				newItems = append(newItems, item)
			}
		}
		result["items"] = newItems
	}

	return result
}

// flattenAnyOfOneOf flattens anyOf/oneOf by selecting the best option based on scoring
func flattenAnyOfOneOf(schema map[string]interface{}) map[string]interface{} {
	if schema == nil {
		return schema
	}

	result := copyMap(schema)

	// Handle anyOf or oneOf
	for _, unionKey := range []string{"anyOf", "oneOf"} {
		if options, ok := result[unionKey].([]interface{}); ok && len(options) > 0 {
			// Collect type names for hint
			var typeNames []string
			var bestOption map[string]interface{}
			bestScore := -1

			for _, option := range options {
				optMap, ok := option.(map[string]interface{})
				if !ok {
					continue
				}

				// Collect type name
				typeName := ""
				if t, ok := optMap["type"].(string); ok {
					typeName = t
				} else if optMap["properties"] != nil {
					typeName = "object"
				}
				if typeName != "" && typeName != "null" {
					typeNames = append(typeNames, typeName)
				}

				// Score and track best option
				score := scoreSchemaOption(optMap)
				if score > bestScore {
					bestScore = score
					bestOption = optMap
				}
			}

			// Remove the union key
			delete(result, unionKey)

			// Merge best option into result
			if bestOption != nil {
				parentDescription, _ := result["description"].(string)

				// Recursively flatten the best option
				flattenedOption := flattenAnyOfOneOf(bestOption)

				// Merge fields from selected option
				for key, value := range flattenedOption {
					if key == "description" {
						if valueStr, ok := value.(string); ok && valueStr != "" && valueStr != parentDescription {
							if parentDescription != "" {
								result["description"] = fmt.Sprintf("%s (%s)", parentDescription, valueStr)
							} else {
								result["description"] = valueStr
							}
						}
					} else {
						if _, exists := result[key]; !exists || key == "type" || key == "properties" || key == "items" {
							result[key] = value
						}
					}
				}

				// Add type hint if multiple types existed
				if len(typeNames) > 1 {
					uniqueTypes := unique(typeNames)
					result = appendDescriptionHint(result, fmt.Sprintf("Accepts: %s", strings.Join(uniqueTypes, " | ")))
				}
			}
		}
	}

	// Recursively process properties
	if props, ok := result["properties"].(map[string]interface{}); ok {
		newProps := make(map[string]interface{})
		for key, value := range props {
			if valueMap, ok := value.(map[string]interface{}); ok {
				newProps[key] = flattenAnyOfOneOf(valueMap)
			} else {
				newProps[key] = value
			}
		}
		result["properties"] = newProps
	}

	// Recursively process items
	if items, ok := result["items"].(map[string]interface{}); ok {
		result["items"] = flattenAnyOfOneOf(items)
	} else if itemsArr, ok := result["items"].([]interface{}); ok {
		newItems := make([]interface{}, 0, len(itemsArr))
		for _, item := range itemsArr {
			if itemMap, ok := item.(map[string]interface{}); ok {
				newItems = append(newItems, flattenAnyOfOneOf(itemMap))
			} else {
				newItems = append(newItems, item)
			}
		}
		result["items"] = newItems
	}

	return result
}

// addEnumHints adds hints for enum values
func addEnumHints(schema map[string]interface{}) map[string]interface{} {
	if schema == nil {
		return schema
	}

	result := copyMap(schema)

	// Add enum hint if present and reasonable size
	if enumArr, ok := result["enum"].([]interface{}); ok && len(enumArr) > 1 && len(enumArr) <= 10 {
		vals := make([]string, 0, len(enumArr))
		for _, v := range enumArr {
			vals = append(vals, fmt.Sprintf("%v", v))
		}
		result = appendDescriptionHint(result, fmt.Sprintf("Allowed: %s", strings.Join(vals, ", ")))
	}

	// Recursively process properties
	if props, ok := result["properties"].(map[string]interface{}); ok {
		newProps := make(map[string]interface{})
		for key, value := range props {
			if valueMap, ok := value.(map[string]interface{}); ok {
				newProps[key] = addEnumHints(valueMap)
			} else {
				newProps[key] = value
			}
		}
		result["properties"] = newProps
	}

	// Recursively process items
	if items, ok := result["items"].(map[string]interface{}); ok {
		result["items"] = addEnumHints(items)
	}

	return result
}

// addAdditionalPropertiesHints adds hints for additionalProperties: false
func addAdditionalPropertiesHints(schema map[string]interface{}) map[string]interface{} {
	if schema == nil {
		return schema
	}

	result := copyMap(schema)

	if result["additionalProperties"] == false {
		result = appendDescriptionHint(result, "No extra properties allowed")
	}

	// Recursively process properties
	if props, ok := result["properties"].(map[string]interface{}); ok {
		newProps := make(map[string]interface{})
		for key, value := range props {
			if valueMap, ok := value.(map[string]interface{}); ok {
				newProps[key] = addAdditionalPropertiesHints(valueMap)
			} else {
				newProps[key] = value
			}
		}
		result["properties"] = newProps
	}

	// Recursively process items
	if items, ok := result["items"].(map[string]interface{}); ok {
		result["items"] = addAdditionalPropertiesHints(items)
	}

	return result
}

// moveConstraintsToDescription moves unsupported constraints to description hints
func moveConstraintsToDescription(schema map[string]interface{}) map[string]interface{} {
	if schema == nil {
		return schema
	}

	constraints := []string{"minLength", "maxLength", "pattern", "minimum", "maximum", "minItems", "maxItems", "format"}

	result := copyMap(schema)

	for _, constraint := range constraints {
		if value, ok := result[constraint]; ok {
			if _, isMap := value.(map[string]interface{}); !isMap {
				result = appendDescriptionHint(result, fmt.Sprintf("%s: %v", constraint, value))
			}
		}
	}

	// Recursively process properties
	if props, ok := result["properties"].(map[string]interface{}); ok {
		newProps := make(map[string]interface{})
		for key, value := range props {
			if valueMap, ok := value.(map[string]interface{}); ok {
				newProps[key] = moveConstraintsToDescription(valueMap)
			} else {
				newProps[key] = value
			}
		}
		result["properties"] = newProps
	}

	// Recursively process items
	if items, ok := result["items"].(map[string]interface{}); ok {
		result["items"] = moveConstraintsToDescription(items)
	}

	return result
}

// flattenTypeArrays flattens array type fields and tracks nullable properties
func flattenTypeArrays(schema map[string]interface{}, nullableProps map[string]bool, currentPropName string) map[string]interface{} {
	if schema == nil {
		return schema
	}

	result := copyMap(schema)

	// Handle array type fields
	if typeArr, ok := result["type"].([]interface{}); ok {
		hasNull := false
		var nonNullTypes []string

		for _, t := range typeArr {
			if tStr, ok := t.(string); ok {
				if tStr == "null" {
					hasNull = true
				} else if tStr != "" {
					nonNullTypes = append(nonNullTypes, tStr)
				}
			}
		}

		// Select first non-null type, or 'string' as fallback
		firstType := "string"
		if len(nonNullTypes) > 0 {
			firstType = nonNullTypes[0]
		}
		result["type"] = firstType

		// Add hint for multiple types
		if len(nonNullTypes) > 1 {
			result = appendDescriptionHint(result, fmt.Sprintf("Accepts: %s", strings.Join(nonNullTypes, " | ")))
		}

		// Track nullable and add hint
		if hasNull {
			result = appendDescriptionHint(result, "nullable")
			if nullableProps != nil && currentPropName != "" {
				nullableProps[currentPropName] = true
			}
		}
	}

	// Recursively process properties, tracking nullable ones
	if props, ok := result["properties"].(map[string]interface{}); ok {
		childNullableProps := make(map[string]bool)
		newProps := make(map[string]interface{})

		for key, value := range props {
			if valueMap, ok := value.(map[string]interface{}); ok {
				newProps[key] = flattenTypeArrays(valueMap, childNullableProps, key)
			} else {
				newProps[key] = value
			}
		}
		result["properties"] = newProps

		// Remove nullable properties from required array
		if required, ok := result["required"].([]interface{}); ok && len(childNullableProps) > 0 {
			newRequired := make([]interface{}, 0)
			for _, prop := range required {
				if propStr, ok := prop.(string); ok {
					if !childNullableProps[propStr] {
						newRequired = append(newRequired, propStr)
					}
				}
			}
			if len(newRequired) == 0 {
				delete(result, "required")
			} else {
				result["required"] = newRequired
			}
		}
	}

	// Recursively process items
	if items, ok := result["items"].(map[string]interface{}); ok {
		result["items"] = flattenTypeArrays(items, nullableProps, "")
	} else if itemsArr, ok := result["items"].([]interface{}); ok {
		newItems := make([]interface{}, 0, len(itemsArr))
		for _, item := range itemsArr {
			if itemMap, ok := item.(map[string]interface{}); ok {
				newItems = append(newItems, flattenTypeArrays(itemMap, nullableProps, ""))
			} else {
				newItems = append(newItems, item)
			}
		}
		result["items"] = newItems
	}

	return result
}

// toGoogleType converts JSON Schema type names to Google's Protobuf-style uppercase type names
func toGoogleType(typeName string) string {
	if typeName == "" {
		return typeName
	}

	typeMap := map[string]string{
		"string":  "STRING",
		"number":  "NUMBER",
		"integer": "INTEGER",
		"boolean": "BOOLEAN",
		"array":   "ARRAY",
		"object":  "OBJECT",
		"null":    "STRING", // Fallback for null type
	}

	if upper, ok := typeMap[strings.ToLower(typeName)]; ok {
		return upper
	}
	return strings.ToUpper(typeName)
}

// Helper functions

func copyMap(m map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range m {
		result[k] = v
	}
	return result
}

func unique(arr []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, len(arr))
	for _, v := range arr {
		if !seen[v] {
			seen[v] = true
			result = append(result, v)
		}
	}
	return result
}
