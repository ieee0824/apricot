package compose

import (
	"os"
	"sort"
	"testing"
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
	if len(cf.Services["web"].Ports) != 1 || cf.Services["web"].Ports[0] != "8080:80" {
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
	if len(web.Ports) != 1 || web.Ports[0] != "9090:80" {
		t.Errorf("expected port 9090:80, got %v", web.Ports)
	}
	home := os.Getenv("HOME")
	expected := home + "/data:/data"
	if len(web.Volumes) != 1 || web.Volumes[0] != expected {
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
