package compose

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type envLookup func(string) (string, bool)

// composeEnvLookup gives shell variables precedence over values in the .env
// alongside the Compose file. The .env supplies interpolation values only; it
// is not automatically passed into containers.
func composeEnvLookup(path string) (envLookup, error) {
	values := make(map[string]string)
	lookup := func(key string) (string, bool) {
		if value, ok := os.LookupEnv(key); ok {
			return value, true
		}
		value, ok := values[key]
		return value, ok
	}
	file, err := os.Open(filepath.Join(filepath.Dir(path), ".env"))
	if os.IsNotExist(err) {
		return lookup, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read .env: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		separator := strings.IndexAny(line, "=:")
		if separator < 0 {
			continue
		}
		key := strings.TrimSpace(line[:separator])
		if !validVariableName(key) {
			return nil, fmt.Errorf("invalid variable name %q in .env", key)
		}
		value := strings.TrimSpace(line[separator+1:])
		if strings.HasPrefix(value, "'") {
			end := strings.LastIndex(value, "'")
			if end < 1 {
				return nil, fmt.Errorf("unterminated single quote for %q in .env", key)
			}
			value = strings.ReplaceAll(value[1:end], `\'`, `'`)
		} else if strings.HasPrefix(value, `"`) {
			end := strings.LastIndex(value, `"`)
			if end < 1 {
				return nil, fmt.Errorf("unterminated double quote for %q in .env", key)
			}
			value, err = strconv.Unquote(value[:end+1])
			if err != nil {
				return nil, fmt.Errorf("invalid quoted value for %q in .env: %w", key, err)
			}
			value, err = interpolateString(value, lookup)
			if err != nil {
				return nil, fmt.Errorf("invalid value for %q in .env: %w", key, err)
			}
		} else {
			if i := strings.Index(value, " #"); i >= 0 {
				value = strings.TrimSpace(value[:i])
			}
			value, err = interpolateString(value, lookup)
			if err != nil {
				return nil, fmt.Errorf("invalid value for %q in .env: %w", key, err)
			}
		}
		values[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read .env: %w", err)
	}
	return lookup, nil
}

// interpolateYAMLValues visits mapping values and sequence items but never
// mapping keys. Single-quoted YAML scalars are literal Compose values.
func interpolateYAMLValues(node *yaml.Node, lookup envLookup) error {
	switch node.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range node.Content {
			if err := interpolateYAMLValues(child, lookup); err != nil {
				return err
			}
		}
	case yaml.MappingNode:
		for i := 1; i < len(node.Content); i += 2 {
			if err := interpolateYAMLValues(node.Content[i], lookup); err != nil {
				return err
			}
		}
	case yaml.ScalarNode:
		if node.Style&yaml.SingleQuotedStyle != 0 {
			return nil
		}
		value, err := interpolateString(node.Value, lookup)
		if err != nil {
			return err
		}
		if value != node.Value {
			node.Value = value
			if node.Style == 0 {
				node.Tag = "" // let YAML resolve an interpolated plain scalar's type
			}
		}
	}
	return nil
}

//mikoto:pure
func validVariableName(s string) bool {
	if s == "" || !variableStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !variablePart(s[i]) {
			return false
		}
	}
	return true
}

//mikoto:pure
func variableStart(c byte) bool { return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

//mikoto:pure
func variablePart(c byte) bool { return variableStart(c) || c >= '0' && c <= '9' }

// interpolateString handles Compose's variable forms and leaves unrelated '$'
// characters alone. Doubled dollars emit a literal '$' without re-expansion.
func interpolateString(s string, lookup envLookup) (string, error) {
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '$' || i+1 == len(s) {
			out.WriteByte(s[i])
			i++
			continue
		}
		if s[i+1] == '$' {
			out.WriteByte('$')
			i += 2
			continue
		}
		if s[i+1] == '{' {
			end, err := interpolationEnd(s, i+2)
			if err != nil {
				return "", err
			}
			value, err := expandBraced(s[i+2:end], lookup)
			if err != nil {
				return "", err
			}
			out.WriteString(value)
			i = end + 1
			continue
		}
		if variableStart(s[i+1]) {
			end := i + 2
			for end < len(s) && variablePart(s[end]) {
				end++
			}
			value, _ := lookup(s[i+1 : end])
			out.WriteString(value)
			i = end
			continue
		}
		out.WriteByte('$')
		i++
	}
	return out.String(), nil
}

func interpolationEnd(s string, start int) (int, error) {
	depth := 1
	for i := start; i < len(s); i++ {
		if s[i] == '$' && i+1 < len(s) && s[i+1] == '{' {
			depth++
			i++
		} else if s[i] == '}' {
			depth--
			if depth == 0 {
				return i, nil
			}
		}
	}
	return 0, fmt.Errorf("unterminated variable expression")
}

func expandBraced(expr string, lookup envLookup) (string, error) {
	end := 0
	for end < len(expr) && variablePart(expr[end]) {
		end++
	}
	if !validVariableName(expr[:end]) {
		return "", fmt.Errorf("invalid variable expression ${%s}", expr)
	}
	name := expr[:end]
	value, set := lookup(name)
	op := expr[end:]
	if op == "" {
		return value, nil
	}
	for _, candidate := range []string{":-", "-", ":?", "?", ":+", "+"} {
		if !strings.HasPrefix(op, candidate) {
			continue
		}
		arg := op[len(candidate):]
		active := set
		if strings.HasPrefix(candidate, ":") {
			active = set && value != ""
		}
		switch candidate {
		case ":-", "-":
			if active {
				return value, nil
			}
			return interpolateString(arg, lookup)
		case ":?", "?":
			if active {
				return value, nil
			}
			return "", fmt.Errorf("%s: %s", name, arg)
		case ":+", "+":
			if active {
				return interpolateString(arg, lookup)
			}
			return "", nil
		}
	}
	return "", fmt.Errorf("unsupported variable expression ${%s}", expr)
}

// expandEnv is kept for existing callers and tests of the basic substitution
// syntax. Load uses the error-returning interpolator directly.
func expandEnv(s string) string {
	value, _ := interpolateString(s, os.LookupEnv)
	return value
}
