package main

import (
	"testing"

	"github.com/ieee0824/apricot/internal/compose"
)

func TestStripScaleIndex(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"web-1", "web"},
		{"web-12", "web"},
		{"web", "web"},
		{"api-server", "api-server"},   // non-numeric suffix kept
		{"api-server-2", "api-server"}, // only trailing index stripped
		{"db", "db"},
	}
	for _, tt := range tests {
		if got := stripScaleIndex(tt.in); got != tt.want {
			t.Errorf("stripScaleIndex(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestServiceNameFromSuffix_NoServiceArgs(t *testing.T) {
	services := map[string]compose.Service{"web": {}, "db": {}}

	// Scaled container suffix resolves to its base service.
	if got := serviceNameFromSuffix("web-2", services, nil); got != "web" {
		t.Errorf("scaled container should match base service, got %q", got)
	}
	// Unscaled container suffix matches directly.
	if got := serviceNameFromSuffix("db", services, nil); got != "db" {
		t.Errorf("unscaled container should match, got %q", got)
	}
	// Container not belonging to any compose service is skipped.
	if got := serviceNameFromSuffix("redis-1", services, nil); got != "" {
		t.Errorf("unknown service should be skipped, got %q", got)
	}
}

func TestServiceNameFromSuffix_ExplicitServiceArgs(t *testing.T) {
	args := []string{"web"}

	// Scaled instances of the requested service match.
	if got := serviceNameFromSuffix("web-1", nil, args); got != "web" {
		t.Errorf("scaled instance of requested service should match, got %q", got)
	}
	if got := serviceNameFromSuffix("web-3", nil, args); got != "web" {
		t.Errorf("scaled instance of requested service should match, got %q", got)
	}
	// A different service is skipped.
	if got := serviceNameFromSuffix("db-1", nil, args); got != "" {
		t.Errorf("non-requested service should be skipped, got %q", got)
	}
}

// A service whose real name ends in "-<number>" must still match when it is not
// scaled, via the full-suffix candidate.
func TestServiceNameFromSuffix_ServiceNameEndingInNumber(t *testing.T) {
	services := map[string]compose.Service{"worker-2": {}}
	if got := serviceNameFromSuffix("worker-2", services, nil); got != "worker-2" {
		t.Errorf("service literally named worker-2 should match, got %q", got)
	}
}
