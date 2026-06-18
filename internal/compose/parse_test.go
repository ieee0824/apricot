package compose

import (
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestExpandEnv(t *testing.T) {
	t.Setenv("SET_VALUE", "hello")
	t.Setenv("EMPTY_VALUE", "")

	got := expandEnv("${SET_VALUE} ${MISSING:-fallback} ${EMPTY_VALUE:-default} ${EMPTY_VALUE-default} ${MISSING-default}")
	want := "hello fallback default  default"
	if got != want {
		t.Fatalf("expandEnv() = %q, want %q", got, want)
	}
}

func TestToStringSlice(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want []string
	}{
		{"nil", nil, nil},
		{"string", "foo", []string{"foo"}},
		{"slice", []interface{}{"a", "b", "c"}, []string{"a", "b", "c"}},
		{"empty slice", []interface{}{}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToStringSlice(tt.in)
			if !stringSliceEqual(got, tt.want) {
				t.Errorf("ToStringSlice(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestToEnvSlice(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want []string
	}{
		{"nil", nil, nil},
		{
			"map with value",
			map[string]interface{}{"FOO": "bar"},
			[]string{"FOO=bar"},
		},
		{
			"map with nil value (key only)",
			map[string]interface{}{"MY_VAR": nil},
			[]string{"MY_VAR"},
		},
		{
			"slice format",
			[]interface{}{"FOO=bar", "BAZ=qux"},
			[]string{"FOO=bar", "BAZ=qux"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToEnvSlice(tt.in)
			sort.Strings(got)
			want := append([]string(nil), tt.want...)
			sort.Strings(want)
			if !stringSliceEqual(got, want) {
				t.Errorf("ToEnvSlice(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestToNetworkNames(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want []string
	}{
		{"nil", nil, nil},
		{
			"slice",
			[]interface{}{"net1", "net2"},
			[]string{"net1", "net2"},
		},
		{
			"map",
			map[string]interface{}{"mynet": nil},
			[]string{"mynet"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToNetworkNames(tt.in)
			sort.Strings(got)
			want := append([]string(nil), tt.want...)
			sort.Strings(want)
			if !stringSliceEqual(got, want) {
				t.Errorf("ToNetworkNames(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSortServices_NoDeps(t *testing.T) {
	services := map[string]Service{
		"web": {Image: "nginx"},
		"db":  {Image: "postgres"},
	}
	order, err := SortServices(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(order) != 2 {
		t.Fatalf("expected 2 services, got %d", len(order))
	}
}

func TestSortServices_DependsOn(t *testing.T) {
	services := map[string]Service{
		"web": {Image: "nginx", DependsOn: []interface{}{"db"}},
		"db":  {Image: "postgres"},
	}
	order, err := SortServices(services)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dbIdx := indexOf(order, "db")
	webIdx := indexOf(order, "web")
	if dbIdx == -1 || webIdx == -1 {
		t.Fatalf("expected both services in order, got %v", order)
	}
	if dbIdx > webIdx {
		t.Errorf("db must come before web, order: %v", order)
	}
}

func TestSortServices_CircularDependency(t *testing.T) {
	services := map[string]Service{
		"a": {Image: "img", DependsOn: []interface{}{"b"}},
		"b": {Image: "img", DependsOn: []interface{}{"a"}},
	}
	_, err := SortServices(services)
	if err == nil {
		t.Error("expected circular dependency error, got nil")
	}
}

func TestLoad(t *testing.T) {
	yaml := `
services:
  web:
    image: nginx:latest
    ports:
      - "8080:80"
    environment:
      - FOO=bar
  db:
    image: postgres:16
    environment:
      POSTGRES_PASSWORD: secret
networks:
  mynet: {}
volumes:
  data: {}
`
	f, err := os.CreateTemp("", "compose-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(yaml)
	f.Close()

	cf, err := Load(f.Name())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if len(cf.Services) != 2 {
		t.Errorf("expected 2 services, got %d", len(cf.Services))
	}
	if cf.Services["web"].Image != "nginx:latest" {
		t.Errorf("expected image nginx:latest, got %s", cf.Services["web"].Image)
	}
	if ports := ToPortList(cf.Services["web"].Ports); len(ports) != 1 || ports[0] != "8080:80" {
		t.Errorf("unexpected ports: %v", cf.Services["web"].Ports)
	}
	if _, ok := cf.Networks["mynet"]; !ok {
		t.Error("expected network mynet")
	}
	if _, ok := cf.Volumes["data"]; !ok {
		t.Error("expected volume data")
	}
}

func TestResolveNetworkName_Normal(t *testing.T) {
	got := ResolveNetworkName("frontend", "myproject", Network{})
	want := "myproject_frontend"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveNetworkName_External(t *testing.T) {
	got := ResolveNetworkName("existing", "myproject", Network{External: true})
	want := "existing"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestResolveNetworkName_ExternalWithName(t *testing.T) {
	got := ResolveNetworkName("mynet", "myproject", Network{External: true, Name: "actual-net"})
	want := "actual-net"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestToBuildConfig_String(t *testing.T) {
	bc := ToBuildConfig("./app")
	if bc == nil {
		t.Fatal("expected non-nil BuildConfig")
	}
	if bc.Context != "./app" {
		t.Errorf("expected context ./app, got %q", bc.Context)
	}
}

func TestToBuildConfig_MapArgsSliceAndLabelsMap(t *testing.T) {
	bc := ToBuildConfig(map[string]interface{}{
		"context":    "./app",
		"dockerfile": "Dockerfile.dev",
		"target":     "builder",
		"no_cache":   true,
		"args": []interface{}{
			"GO_VERSION=1.24",
			"APP_ENV=prod",
		},
		"labels": map[string]interface{}{
			"org.example.role": "api",
		},
	})
	if bc == nil {
		t.Fatal("expected non-nil BuildConfig")
	}
	if bc.Context != "./app" {
		t.Errorf("expected context ./app, got %q", bc.Context)
	}
	if bc.Dockerfile != "Dockerfile.dev" {
		t.Errorf("expected dockerfile Dockerfile.dev, got %q", bc.Dockerfile)
	}
	if bc.Target != "builder" {
		t.Errorf("expected target builder, got %q", bc.Target)
	}
	if !bc.NoCache {
		t.Error("expected no_cache to be true")
	}
	if bc.Args["GO_VERSION"] != "1.24" || bc.Args["APP_ENV"] != "prod" {
		t.Errorf("unexpected build args: %#v", bc.Args)
	}
	if bc.Labels["org.example.role"] != "api" {
		t.Errorf("unexpected labels: %#v", bc.Labels)
	}
}

func TestToUlimitSlice(t *testing.T) {
	got := ToUlimitSlice(map[string]interface{}{
		"nofile": 1024,
		"nproc": map[string]interface{}{
			"soft": 512.0,
			"hard": 1024.0,
		},
		"memlock": map[string]interface{}{
			"soft": 64,
		},
	})
	sort.Strings(got)
	want := []string{"memlock=64", "nofile=1024", "nproc=512:1024"}
	if !stringSliceEqual(got, want) {
		t.Fatalf("ToUlimitSlice() = %v, want %v", got, want)
	}
}

func TestSortServices_MissingDependency(t *testing.T) {
	services := map[string]Service{
		"web": {DependsOn: []interface{}{"db"}},
	}
	_, err := SortServices(services)
	if err == nil {
		t.Fatal("expected error for missing dependency")
	}
}

func TestToBuildConfig_Nil(t *testing.T) {
	if ToBuildConfig(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestToBuildConfig_Map(t *testing.T) {
	input := map[string]interface{}{
		"context":    "./src",
		"dockerfile": "Dockerfile.dev",
		"target":     "builder",
		"no_cache":   true,
		"args":       map[string]interface{}{"ENV": "prod"},
		"labels":     map[string]interface{}{"app": "myapp"},
	}
	bc := ToBuildConfig(input)
	if bc == nil {
		t.Fatal("expected non-nil BuildConfig")
	}
	if bc.Context != "./src" {
		t.Errorf("context: got %q", bc.Context)
	}
	if bc.Dockerfile != "Dockerfile.dev" {
		t.Errorf("dockerfile: got %q", bc.Dockerfile)
	}
	if bc.Target != "builder" {
		t.Errorf("target: got %q", bc.Target)
	}
	if !bc.NoCache {
		t.Error("no_cache should be true")
	}
	if bc.Args["ENV"] != "prod" {
		t.Errorf("args: got %v", bc.Args)
	}
	if bc.Labels["app"] != "myapp" {
		t.Errorf("labels: got %v", bc.Labels)
	}
}

func TestToBuildConfig_ArgsSlice(t *testing.T) {
	input := map[string]interface{}{
		"args": []interface{}{"FOO=bar", "BAZ=qux"},
	}
	bc := ToBuildConfig(input)
	if bc.Args["FOO"] != "bar" || bc.Args["BAZ"] != "qux" {
		t.Errorf("args slice parse failed: %v", bc.Args)
	}
}

func TestLoad_ExpandEnv(t *testing.T) {
	t.Setenv("TEST_APRICOT_IMAGE", "myapp:v2")
	t.Setenv("TEST_APRICOT_PORT", "9090")

	yaml := `
services:
  web:
    image: ${TEST_APRICOT_IMAGE}
    ports:
      - "${TEST_APRICOT_PORT}:80"
    volumes:
      - ${HOME}/data:/data
`
	f, err := os.CreateTemp("", "compose-env-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(yaml)
	f.Close()

	cf, err := Load(f.Name())
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	web := cf.Services["web"]
	if web.Image != "myapp:v2" {
		t.Errorf("expected image myapp:v2, got %q", web.Image)
	}
	if ports := ToPortList(web.Ports); len(ports) != 1 || ports[0] != "9090:80" {
		t.Errorf("expected port 9090:80, got %v", web.Ports)
	}
	home := os.Getenv("HOME")
	expected := home + "/data:/data"
	if vols := ToVolumeList(web.Volumes); len(vols) != 1 || vols[0] != expected {
		t.Errorf("expected volume %q, got %v", expected, web.Volumes)
	}
}

func TestExpandEnv_DefaultValue(t *testing.T) {
	os.Unsetenv("TEST_APRICOT_UNSET")
	t.Setenv("TEST_APRICOT_SET", "fromenv")

	cases := []struct {
		input string
		want  string
	}{
		{"${TEST_APRICOT_UNSET:-fallback}", "fallback"},
		{"${TEST_APRICOT_SET:-fallback}", "fromenv"},
		{"${TEST_APRICOT_UNSET-fallback2}", "fallback2"},
		{"${TEST_APRICOT_SET-fallback2}", "fromenv"},
		{"$TEST_APRICOT_SET", "fromenv"},
	}
	for _, c := range cases {
		got := expandEnv(c.input)
		if got != c.want {
			t.Errorf("expandEnv(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/docker-compose.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// helpers

func TestToUlimitSlice_Shorthand(t *testing.T) {
	v := map[string]interface{}{
		"nproc": 65535,
	}
	result := ToUlimitSlice(v)
	if len(result) != 1 || result[0] != "nproc=65535" {
		t.Errorf("expected [nproc=65535], got %v", result)
	}
}

func TestToUlimitSlice_LongForm(t *testing.T) {
	v := map[string]interface{}{
		"nofile": map[string]interface{}{
			"soft": 1024,
			"hard": 2048,
		},
	}
	result := ToUlimitSlice(v)
	if len(result) != 1 || result[0] != "nofile=1024:2048" {
		t.Errorf("expected [nofile=1024:2048], got %v", result)
	}
}

func TestToUlimitSlice_Nil(t *testing.T) {
	if ToUlimitSlice(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestToStringSlice_DNSSearch(t *testing.T) {
	svc := Service{Image: "myapp", DNSSearch: []interface{}{"example.com"}}
	result := ToStringSlice(svc.DNSSearch)
	if len(result) != 1 || result[0] != "example.com" {
		t.Errorf("expected [example.com], got %v", result)
	}
}

func TestToInt_Int(t *testing.T) {
	v, ok := toInt(42)
	if !ok || v != 42 {
		t.Errorf("got (%d, %v), want (42, true)", v, ok)
	}
}

func TestToInt_Float64(t *testing.T) {
	v, ok := toInt(float64(3))
	if !ok || v != 3 {
		t.Errorf("got (%d, %v), want (3, true)", v, ok)
	}
}

func TestToInt_Invalid(t *testing.T) {
	_, ok := toInt("not a number")
	if ok {
		t.Error("expected false for string input")
	}
}

func TestLoad_InvalidYAML(t *testing.T) {
	// Malformed YAML: an unclosed bracket in the image value.
	yaml := `
services:
  web:
    image: [nginx:latest
`
	f, err := os.CreateTemp("", "compose-invalid-*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(yaml)
	f.Close()

	_, err = Load(f.Name())
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse") {
		t.Errorf("expected error containing %q, got %q", "failed to parse", err.Error())
	}
}

func TestToUlimitSlice_AllCases(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want []string
	}{
		{"nil", nil, nil},
		{"shorthand int", map[string]interface{}{"nproc": 65535}, []string{"nproc=65535"}},
		{"shorthand float64", map[string]interface{}{"nproc": float64(65535)}, []string{"nproc=65535"}},
		{
			"long form soft/hard",
			map[string]interface{}{"nofile": map[string]interface{}{"soft": 1024, "hard": 2048}},
			[]string{"nofile=1024:2048"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToUlimitSlice(tt.in)
			if !stringSliceEqual(got, tt.want) {
				t.Errorf("ToUlimitSlice(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestExpandEnv_EmptyVarName(t *testing.T) {
	// An empty variable name before the default form must NOT be treated as a
	// valid default expression. It is equivalent to os.Getenv("") (empty string),
	// so the default value is intentionally NOT yielded.
	cases := []string{
		"${:-fallback}",
		"${-fallback}",
	}
	for _, in := range cases {
		got := expandEnv(in)
		if got == "fallback" {
			t.Errorf("expandEnv(%q) = %q, default value must not be silently yielded for empty var name", in, got)
		}
		if got != "" {
			t.Errorf("expandEnv(%q) = %q, want %q", in, got, "")
		}
	}
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func indexOf(slice []string, s string) int {
	for i, v := range slice {
		if v == s {
			return i
		}
	}
	return -1
}

func TestToStringMap_SliceForm(t *testing.T) {
	got := toStringMap([]interface{}{
		"FOO=bar",
		"BAZ=qux=extra", // value may contain '='
		"NOEQUALS",      // malformed: no '=' -> dropped
		"=value",        // malformed: empty key -> dropped
		123,             // non-string -> dropped
	})
	want := map[string]string{
		"FOO": "bar",
		"BAZ": "qux=extra",
	}
	if len(got) != len(want) {
		t.Fatalf("toStringMap() = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("toStringMap()[%q] = %q, want %q", k, got[k], v)
		}
	}
}

func TestToStringMap_MapForm(t *testing.T) {
	got := toStringMap(map[string]interface{}{"FOO": "bar", "NUM": 42})
	if got["FOO"] != "bar" || got["NUM"] != "42" {
		t.Errorf("toStringMap() = %v", got)
	}
}

// The slice converters must drop (and warn about) non-string items rather than
// panic or include zero values. These exercise the warn branches.
func TestToStringSlice_DropsNonString(t *testing.T) {
	got := ToStringSlice([]interface{}{"a", 1, "b", nil})
	if !stringSliceEqual(got, []string{"a", "b"}) {
		t.Errorf("ToStringSlice() = %v, want [a b]", got)
	}
}

func TestToEnvSlice_DropsNonString(t *testing.T) {
	got := ToEnvSlice([]interface{}{"FOO=bar", 99})
	if !stringSliceEqual(got, []string{"FOO=bar"}) {
		t.Errorf("ToEnvSlice() = %v, want [FOO=bar]", got)
	}
}

func TestToNetworkNames_DropsNonString(t *testing.T) {
	got := ToNetworkNames([]interface{}{"frontend", 7})
	if !stringSliceEqual(got, []string{"frontend"}) {
		t.Errorf("ToNetworkNames() = %v, want [frontend]", got)
	}
}

func TestToPortList(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want []string
	}{
		{"nil", nil, nil},
		{"short strings", []interface{}{"8080:80", "443:443/tcp"}, []string{"8080:80", "443:443/tcp"}},
		{"[]string form", []string{"8080:80"}, []string{"8080:80"}},
		{"long published+target", []interface{}{map[string]interface{}{"target": 80, "published": 8080}}, []string{"8080:80"}},
		{"long with protocol", []interface{}{map[string]interface{}{"target": 80, "published": 8080, "protocol": "tcp"}}, []string{"8080:80/tcp"}},
		{"long with host_ip", []interface{}{map[string]interface{}{"target": 80, "published": 8080, "host_ip": "127.0.0.1"}}, []string{"127.0.0.1:8080:80"}},
		{"long host_ip without published", []interface{}{map[string]interface{}{"target": 80, "host_ip": "127.0.0.1"}}, []string{"127.0.0.1:80:80"}},
		{"long host_ip without published, with proto", []interface{}{map[string]interface{}{"target": 80, "host_ip": "127.0.0.1", "protocol": "udp"}}, []string{"127.0.0.1:80:80/udp"}},
		{"invalid protocol omitted", []interface{}{map[string]interface{}{"target": 80, "published": 8080, "protocol": true}}, []string{"8080:80"}},
		{"long target only", []interface{}{map[string]interface{}{"target": 80}}, []string{"80"}},
		{"mixed short and long", []interface{}{"5432:5432", map[string]interface{}{"target": 80, "published": 8080}}, []string{"5432:5432", "8080:80"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToPortList(tt.in); !stringSliceEqual(got, tt.want) {
				t.Errorf("ToPortList(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestToVolumeList(t *testing.T) {
	tests := []struct {
		name string
		in   interface{}
		want []string
	}{
		{"nil", nil, nil},
		{"short strings", []interface{}{"./data:/data", "named:/var"}, []string{"./data:/data", "named:/var"}},
		{"[]string form", []string{"/a:/b"}, []string{"/a:/b"}},
		{"long bind", []interface{}{map[string]interface{}{"type": "bind", "source": "./data", "target": "/data"}}, []string{"./data:/data"}},
		{"long read_only", []interface{}{map[string]interface{}{"source": "./cfg", "target": "/cfg", "read_only": true}}, []string{"./cfg:/cfg:ro"}},
		{"long anonymous (target only)", []interface{}{map[string]interface{}{"target": "/cache"}}, []string{"/cache"}},
		{"long anonymous read_only", []interface{}{map[string]interface{}{"target": "/cache", "read_only": true}}, []string{"/cache:ro"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToVolumeList(tt.in); !stringSliceEqual(got, tt.want) {
				t.Errorf("ToVolumeList(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestUnsupportedKeyWarnings(t *testing.T) {
	yaml := `
services:
  web:
    image: nginx
    ports: ["80:80"]
    healthcheck:
      test: ["CMD", "true"]
    restart: always
    deploy:
      replicas: 3
  db:
    image: postgres
    init: true
`
	got := unsupportedKeyWarnings(yaml)
	want := []string{
		`service "db": "init" is not supported by apricot and will be ignored`,
		`service "web": "deploy" is not supported by apricot and will be ignored`,
		`service "web": "restart" is not supported by apricot and will be ignored`,
	}
	if !stringSliceEqual(got, want) {
		t.Errorf("unsupportedKeyWarnings() =\n  %v\nwant\n  %v", got, want)
	}
}

func TestUnsupportedKeyWarnings_AllSupported(t *testing.T) {
	yaml := `
services:
  web:
    image: nginx
    ports: ["80:80"]
    volumes: ["./data:/data"]
    environment: {FOO: bar}
    platform: linux/arm64
`
	if got := unsupportedKeyWarnings(yaml); len(got) != 0 {
		t.Errorf("expected no warnings, got %v", got)
	}
}

func TestToDependsOnConditions(t *testing.T) {
	// List form: all default to service_started.
	list := ToDependsOnConditions([]interface{}{"db", "redis"})
	if list["db"] != "service_started" || list["redis"] != "service_started" {
		t.Errorf("list form = %v", list)
	}
	// Map form with conditions.
	m := ToDependsOnConditions(map[string]interface{}{
		"db":    map[string]interface{}{"condition": "service_healthy"},
		"cache": map[string]interface{}{"condition": "service_started"},
		"bare":  nil,
	})
	if m["db"] != "service_healthy" {
		t.Errorf("db condition = %q, want service_healthy", m["db"])
	}
	if m["cache"] != "service_started" {
		t.Errorf("cache condition = %q", m["cache"])
	}
	if m["bare"] != "service_started" {
		t.Errorf("bare (no condition) = %q, want service_started", m["bare"])
	}
}

func TestHealthcheckNormalize(t *testing.T) {
	t.Run("nil and disabled have no check", func(t *testing.T) {
		var hc *Healthcheck
		if _, ok := hc.Normalize(); ok {
			t.Error("nil healthcheck should have no check")
		}
		if _, ok := (&Healthcheck{Disable: true, Test: "true"}).Normalize(); ok {
			t.Error("disabled healthcheck should have no check")
		}
		if _, ok := (&Healthcheck{Test: []interface{}{"NONE"}}).Normalize(); ok {
			t.Error("NONE test should have no check")
		}
	})

	t.Run("CMD form runs argv directly", func(t *testing.T) {
		spec, ok := (&Healthcheck{Test: []interface{}{"CMD", "curl", "-f", "http://x"}}).Normalize()
		if !ok {
			t.Fatal("expected a check")
		}
		if !stringSliceEqual(spec.Cmd, []string{"curl", "-f", "http://x"}) {
			t.Errorf("cmd = %v", spec.Cmd)
		}
	})

	t.Run("CMD-SHELL and string run via /bin/sh -c", func(t *testing.T) {
		shell, _ := (&Healthcheck{Test: []interface{}{"CMD-SHELL", "pg_isready -U me"}}).Normalize()
		if !stringSliceEqual(shell.Cmd, []string{"/bin/sh", "-c", "pg_isready -U me"}) {
			t.Errorf("cmd-shell = %v", shell.Cmd)
		}
		str, _ := (&Healthcheck{Test: "pg_isready"}).Normalize()
		if !stringSliceEqual(str.Cmd, []string{"/bin/sh", "-c", "pg_isready"}) {
			t.Errorf("string form = %v", str.Cmd)
		}
	})

	t.Run("durations and retries with defaults", func(t *testing.T) {
		spec, _ := (&Healthcheck{Test: "true", Interval: "5s", Timeout: "2s", Retries: 4, StartPeriod: "10s"}).Normalize()
		if spec.Interval != 5*time.Second || spec.Timeout != 2*time.Second || spec.Retries != 4 || spec.StartPeriod != 10*time.Second {
			t.Errorf("spec = %+v", spec)
		}
		def, _ := (&Healthcheck{Test: "true"}).Normalize()
		if def.Interval != 30*time.Second || def.Timeout != 30*time.Second || def.Retries != 3 || def.StartPeriod != 0 {
			t.Errorf("defaults = %+v", def)
		}
	})
}
