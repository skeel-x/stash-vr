package filter

import (
	"fmt"
	"strconv"
	"strings"
)

func Get[T any](m any, path string) *T {
	m, ok := m.(map[string]interface{})
	if !ok {
		return nil
	}
	parts := strings.Split(path, ".")
	var current = m

	for _, key := range parts {
		node, ok := current.(map[string]interface{})
		if !ok {
			return nil
		}
		val, exists := node[key]
		if !exists {
			return nil
		}
		current = val
	}

	var zero T
	switch any(zero).(type) {
	case string:
		var out string
		if s, ok := current.(string); ok {
			out = s
		} else {
			out = fmt.Sprintf("%v", current)
		}
		v := any(out).(T)
		return &v

	case int:
		switch num := current.(type) {
		case float64:
			out := int(num)
			r := any(out).(T)
			return &r
		case float32:
			out := int(num)
			r := any(out).(T)
			return &r
		case int:
			r := any(num).(T)
			return &r
		default:
			return nil
		}

	case bool:
		var out string
		if s, ok := current.(string); ok {
			out = s
		} else {
			out = fmt.Sprintf("%v", current)
		}
		pb, err := strconv.ParseBool(out)
		if err != nil {
			return nil
		}
		v := any(pb).(T)
		return &v

	default:
		if v, ok := current.(T); ok {
			return &v
		}
		return nil
	}
}

func GetOr[T any](m any, path string, def T) T {
	if v := Get[T](m, path); v != nil {
		return *v
	}
	return def
}

// require returns the value at path, or an error naming the path when it
// is missing or not a T.
func require[T any](m any, path string) (T, error) {
	if v := Get[T](m, path); v != nil {
		return *v, nil
	}
	var zero T
	return zero, fmt.Errorf("%s is missing or not a %T", path, zero)
}
