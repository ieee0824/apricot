package compose

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Load reads and parses a docker-compose.yaml file.
func Load(path string) (*ComposeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	var cf ComposeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}

	return &cf, nil
}

// ToStringSlice converts an interface{} that is either a string or []interface{} to []string.
func ToStringSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case string:
		return []string{val}
	case []interface{}:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// ToEnvSlice converts environment field (map or []string) to KEY=VALUE slice.
func ToEnvSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case map[string]interface{}:
		result := make([]string, 0, len(val))
		for k, v := range val {
			if v == nil {
				result = append(result, k)
			} else {
				result = append(result, fmt.Sprintf("%s=%v", k, v))
			}
		}
		return result
	case []interface{}:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// ToNetworkNames converts networks field ([]string or map) to network name slice.
func ToNetworkNames(v interface{}) []string {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case []interface{}:
		result := make([]string, 0, len(val))
		for _, item := range val {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case map[string]interface{}:
		result := make([]string, 0, len(val))
		for k := range val {
			result = append(result, k)
		}
		return result
	}
	return nil
}

// ToDependsOn converts depends_on field ([]string or map) to service name slice.
func ToDependsOn(v interface{}) []string {
	return ToNetworkNames(v)
}

// SortServices returns service names sorted by depends_on dependency order.
func SortServices(services map[string]Service) ([]string, error) {
	visited := make(map[string]bool)
	inStack := make(map[string]bool)
	var order []string

	var visit func(name string) error
	visit = func(name string) error {
		if inStack[name] {
			return fmt.Errorf("circular dependency detected: %s", name)
		}
		if visited[name] {
			return nil
		}
		inStack[name] = true
		svc, ok := services[name]
		if !ok {
			return fmt.Errorf("service not found: %s", name)
		}
		for _, dep := range ToDependsOn(svc.DependsOn) {
			if err := visit(dep); err != nil {
				return err
			}
		}
		inStack[name] = false
		visited[name] = true
		order = append(order, name)
		return nil
	}

	for name := range services {
		if err := visit(name); err != nil {
			return nil, err
		}
	}

	return order, nil
}
