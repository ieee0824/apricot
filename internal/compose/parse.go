package compose

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// expandEnv expands environment variables supporting ${VAR:-default} and ${VAR-default} syntax.
func expandEnv(s string) string {
	return os.Expand(s, func(key string) string {
		if i := strings.Index(key, ":-"); i >= 0 {
			name, def := key[:i], key[i+2:]
			if v := os.Getenv(name); v != "" {
				return v
			}
			return def
		}
		if i := strings.Index(key, "-"); i >= 0 {
			name, def := key[:i], key[i+1:]
			if v, ok := os.LookupEnv(name); ok {
				return v
			}
			return def
		}
		return os.Getenv(key)
	})
}

// Load reads and parses a docker-compose.yaml file.
// Environment variables ($VAR, ${VAR}, ${VAR:-default}) in the file are expanded before parsing.
func Load(path string) (*ComposeFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	expanded := expandEnv(string(data))

	var cf ComposeFile
	if err := yaml.Unmarshal([]byte(expanded), &cf); err != nil {
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

// ToBuildConfig converts the build: field (string or map) to a BuildConfig.
// Returns nil if v is nil (no build defined).
func ToBuildConfig(v interface{}) *BuildConfig {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case string:
		return &BuildConfig{Context: val}
	case map[string]interface{}:
		bc := &BuildConfig{}
		if ctx, ok := val["context"].(string); ok {
			bc.Context = ctx
		}
		if df, ok := val["dockerfile"].(string); ok {
			bc.Dockerfile = df
		}
		if target, ok := val["target"].(string); ok {
			bc.Target = target
		}
		if noCache, ok := val["no_cache"].(bool); ok {
			bc.NoCache = noCache
		}
		bc.Args = toStringMap(val["args"])
		bc.Labels = toStringMap(val["labels"])
		return bc
	}
	return nil
}

// toStringMap converts a map[string]interface{} or []interface{} (KEY=VAL) to map[string]string.
func toStringMap(v interface{}) map[string]string {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case map[string]interface{}:
		result := make(map[string]string, len(val))
		for k, v := range val {
			result[k] = fmt.Sprintf("%v", v)
		}
		return result
	case []interface{}:
		result := make(map[string]string)
		for _, item := range val {
			if s, ok := item.(string); ok {
				for i, c := range s {
					if c == '=' {
						result[s[:i]] = s[i+1:]
						break
					}
				}
			}
		}
		return result
	}
	return nil
}

// ToUlimitSlice converts the ulimits: field to a slice of "type=soft:hard" strings.
// Supports both shorthand (int) and long form ({soft: N, hard: N}).
func ToUlimitSlice(v interface{}) []string {
	if v == nil {
		return nil
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(m))
	for name, val := range m {
		switch u := val.(type) {
		case int:
			result = append(result, fmt.Sprintf("%s=%d", name, u))
		case map[string]interface{}:
			soft, hasSoft := toInt(u["soft"])
			hard, hasHard := toInt(u["hard"])
			if hasSoft && hasHard {
				result = append(result, fmt.Sprintf("%s=%d:%d", name, soft, hard))
			} else if hasSoft {
				result = append(result, fmt.Sprintf("%s=%d", name, soft))
			}
		}
	}
	return result
}

// toInt tries to extract an int from interface{} (handles int and float64 from YAML).
func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	}
	return 0, false
}

// ResolveNetworkName returns the actual network name to pass to --network.
// External networks use their own name (or the name: override), not the project prefix.
func ResolveNetworkName(key, projectName string, net Network) string {
	if net.External {
		if net.Name != "" {
			return net.Name
		}
		return key
	}
	return projectName + "_" + key
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
