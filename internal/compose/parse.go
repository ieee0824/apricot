package compose

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// warnf prints a parsing warning to stderr so that silently malformed compose
// values (which would otherwise be dropped) are surfaced to the user.
func warnf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Warning: "+format+"\n", args...)
}

// expandEnv expands environment variables supporting ${VAR:-default} and ${VAR-default} syntax.
func expandEnv(s string) string {
	return os.Expand(s, func(key string) string {
		if i := strings.Index(key, ":-"); i >= 0 {
			name, def := key[:i], key[i+2:]
			if name == "" {
				return os.Getenv("")
			}
			if v := os.Getenv(name); v != "" {
				return v
			}
			return def
		}
		if i := strings.Index(key, "-"); i >= 0 {
			name, def := key[:i], key[i+1:]
			if name == "" {
				return os.Getenv("")
			}
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

	for _, w := range unsupportedKeyWarnings(expanded) {
		warnf("%s", w)
	}

	return &cf, nil
}

// handledServiceKeys are the docker-compose service keys apricot actually acts
// on. Any other key present in a service is parsed-but-ignored, so it is
// surfaced as a warning rather than being silently dropped.
var handledServiceKeys = map[string]bool{
	"image": true, "build": true, "command": true, "entrypoint": true,
	"environment": true, "env_file": true, "ports": true, "volumes": true,
	"networks": true, "labels": true, "working_dir": true, "user": true,
	"cpus": true, "mem_limit": true, "stdin_open": true, "tty": true,
	"depends_on": true, "container_name": true, "read_only": true,
	"tmpfs": true, "dns": true, "dns_search": true, "dns_opt": true,
	"platform": true, "healthcheck": true, "init": true,
}

// unsupportedKeyWarnings returns one warning per service key apricot does not
// handle (e.g. deploy, restart, ulimits). Output is sorted
// for stable, testable results.
func unsupportedKeyWarnings(expanded string) []string {
	var raw struct {
		Services map[string]map[string]yaml.Node `yaml:"services"`
	}
	if err := yaml.Unmarshal([]byte(expanded), &raw); err != nil {
		return nil
	}
	services := make([]string, 0, len(raw.Services))
	for name := range raw.Services {
		services = append(services, name)
	}
	sort.Strings(services)

	var warnings []string
	for _, name := range services {
		keys := make([]string, 0)
		for k := range raw.Services[name] {
			if !handledServiceKeys[k] {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		for _, k := range keys {
			warnings = append(warnings, fmt.Sprintf("service %q: %q is not supported by apricot and will be ignored", name, k))
		}
	}
	return warnings
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
			} else {
				warnf("ignoring non-string list item %v", item)
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
			} else {
				warnf("ignoring non-string environment item %v", item)
			}
		}
		return result
	}
	return nil
}

// ToPortList converts the ports field to short "host:container[/proto]" specs.
// docker compose accepts both the short string form ("8080:80") and the long
// mapping form ({target:, published:, protocol:, host_ip:}); both are handled.
func ToPortList(v interface{}) []string {
	switch val := v.(type) {
	case nil:
		return nil
	case string:
		return []string{val}
	case []string:
		return append([]string(nil), val...)
	case []interface{}:
		result := make([]string, 0, len(val))
		for _, item := range val {
			switch p := item.(type) {
			case string:
				result = append(result, p)
			case map[string]interface{}:
				if spec := portMapToSpec(p); spec != "" {
					result = append(result, spec)
				} else {
					warnf("ignoring port entry without a target: %v", p)
				}
			default:
				warnf("ignoring unsupported port entry %v", item)
			}
		}
		return result
	}
	return nil
}

// portMapToSpec renders a long-form port map as "[host_ip:][published:]target[/proto]".
func portMapToSpec(m map[string]interface{}) string {
	target := scalarToString(m["target"])
	if target == "" {
		return ""
	}
	published := scalarToString(m["published"])
	hostIP := scalarToString(m["host_ip"])
	// A host_ip needs an explicit host port to form a valid 3-field spec
	// (host_ip:published:target); otherwise "host_ip:target" is ambiguous and
	// rejected by the CLI. Default the published port to the target port.
	if hostIP != "" && published == "" {
		published = target
	}
	spec := target
	if published != "" {
		spec = published + ":" + target
	}
	if hostIP != "" {
		spec = hostIP + ":" + spec
	}
	if proto := scalarToString(m["protocol"]); proto != "" {
		if proto == "tcp" || proto == "udp" {
			spec += "/" + proto
		} else {
			warnf("ignoring invalid port protocol %q (must be tcp or udp)", proto)
		}
	}
	return spec
}

// ToVolumeList converts the volumes field to short "source:target[:ro]" specs.
// docker compose accepts both the short string form and the long mapping form
// ({type:, source:, target:, read_only:}); both are handled.
func ToVolumeList(v interface{}) []string {
	switch val := v.(type) {
	case nil:
		return nil
	case string:
		return []string{val}
	case []string:
		return append([]string(nil), val...)
	case []interface{}:
		result := make([]string, 0, len(val))
		for _, item := range val {
			switch vol := item.(type) {
			case string:
				result = append(result, vol)
			case map[string]interface{}:
				if spec := volumeMapToSpec(vol); spec != "" {
					result = append(result, spec)
				} else {
					warnf("ignoring volume entry without a target: %v", vol)
				}
			default:
				warnf("ignoring unsupported volume entry %v", item)
			}
		}
		return result
	}
	return nil
}

// volumeMapToSpec renders a long-form volume map as "[source:]target[:ro]".
func volumeMapToSpec(m map[string]interface{}) string {
	target := scalarToString(m["target"])
	if target == "" {
		return ""
	}
	spec := target
	if source := scalarToString(m["source"]); source != "" {
		spec = source + ":" + target
	}
	if ro, _ := m["read_only"].(bool); ro {
		spec += ":ro"
	}
	return spec
}

// scalarToString renders a YAML scalar (string/int/float/bool) as a string.
func scalarToString(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case float64:
		return strconv.Itoa(int(x))
	case bool:
		return strconv.FormatBool(x)
	default:
		return fmt.Sprintf("%v", x)
	}
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
			} else {
				warnf("ignoring non-string network/list item %v", item)
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

// ToDependsOnConditions returns each dependency service mapped to its startup
// condition. The list form and bare map keys default to "service_started"; the
// long map form ({db: {condition: service_healthy}}) captures the condition.
func ToDependsOnConditions(v interface{}) map[string]string {
	result := map[string]string{}
	switch val := v.(type) {
	case []interface{}:
		for _, item := range val {
			if s, ok := item.(string); ok {
				result[s] = "service_started"
			}
		}
	case map[string]interface{}:
		for k, cv := range val {
			cond := "service_started"
			if m, ok := cv.(map[string]interface{}); ok {
				if c, ok := m["condition"].(string); ok && c != "" {
					cond = c
				}
			}
			result[k] = cond
		}
	}
	return result
}

// HealthcheckSpec is a normalized, runnable healthcheck.
type HealthcheckSpec struct {
	Cmd         []string // argv to exec inside the container
	Interval    time.Duration
	Timeout     time.Duration
	Retries     int
	StartPeriod time.Duration
}

// Normalize resolves a Healthcheck into a runnable spec. The second return value
// is false when there is no check to run (nil, disabled, or test: ["NONE"]).
func (h *Healthcheck) Normalize() (HealthcheckSpec, bool) {
	if h == nil || h.Disable {
		return HealthcheckSpec{}, false
	}
	cmd := healthcheckCmd(h.Test)
	if len(cmd) == 0 {
		return HealthcheckSpec{}, false
	}
	return HealthcheckSpec{
		Cmd:         cmd,
		Interval:    parseDurationOr(h.Interval, 30*time.Second),
		Timeout:     parseDurationOr(h.Timeout, 30*time.Second),
		Retries:     orInt(h.Retries, 3),
		StartPeriod: parseDurationOr(h.StartPeriod, 0),
	}, true
}

// healthcheckCmd converts the polymorphic test field into the argv to exec.
// Returns nil when the check is absent or explicitly disabled via ["NONE"].
func healthcheckCmd(test interface{}) []string {
	switch t := test.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{"/bin/sh", "-c", t}
	case []interface{}:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				parts = append(parts, s)
			}
		}
		if len(parts) == 0 {
			return nil
		}
		switch parts[0] {
		case "NONE":
			return nil
		case "CMD":
			return parts[1:]
		case "CMD-SHELL":
			if len(parts) < 2 {
				return nil
			}
			return []string{"/bin/sh", "-c", strings.Join(parts[1:], " ")}
		default:
			return parts // lenient: treat as a bare exec form
		}
	}
	return nil
}

func parseDurationOr(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		warnf("invalid healthcheck duration %q, using %s", s, def)
		return def
	}
	return d
}

func orInt(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
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
			s, ok := item.(string)
			if !ok {
				warnf("ignoring non-string entry %v", item)
				continue
			}
			i := strings.IndexByte(s, '=')
			if i <= 0 { // no '=' separator, or empty key ("=value")
				warnf("ignoring malformed entry %q (expected KEY=VALUE)", s)
				continue
			}
			result[s[:i]] = s[i+1:]
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
		case float64:
			result = append(result, fmt.Sprintf("%s=%d", name, int(u)))
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
