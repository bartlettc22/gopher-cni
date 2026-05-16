package utils

import (
	"os"
	"strconv"
)

// GetEnv gets an environment variable with a default value
// The return type is inferred from the type of defaultValue
func GetEnv[T any](key string, defaultValue T) T {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}

	// Type switch based on the default value type
	var result any
	switch any(defaultValue).(type) {
	case string:
		result = value
	case int:
		if intVal, err := strconv.Atoi(value); err == nil {
			result = intVal
		} else {
			return defaultValue
		}
	case bool:
		if boolVal, err := strconv.ParseBool(value); err == nil {
			result = boolVal
		} else {
			return defaultValue
		}
	default:
		return defaultValue
	}

	return result.(T)
}
