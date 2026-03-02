package compose

import (
	"os"
	"sort"
	"testing"
)

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

func TestLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/docker-compose.yaml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// helpers

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
