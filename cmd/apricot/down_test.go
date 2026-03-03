package main

import (
	"sort"
	"testing"

	"github.com/ieee0824/apricot/internal/compose"
	"github.com/ieee0824/apricot/internal/runner"
)

func TestContainersForProject(t *testing.T) {
	containers := []runner.Container{
		{Name: "myproject-web"},
		{Name: "myproject-db"},
		{Name: "other-web"},
		{Name: "myproject-extra"},
	}

	got := containersForProject(containers, "myproject")
	if len(got) != 3 {
		t.Fatalf("expected 3 containers, got %d: %v", len(got), got)
	}
	for _, c := range got {
		if c.Name == "other-web" {
			t.Errorf("other-web should not be included")
		}
	}
}

func TestContainersForProject_Empty(t *testing.T) {
	got := containersForProject(nil, "myproject")
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestNetworkNamesForProject(t *testing.T) {
	networks := map[string]compose.Network{
		"frontend": {},
		"backend":  {},
	}
	got := networkNamesForProject(networks, "myproject")
	sort.Strings(got)
	want := []string{"myproject_backend", "myproject_frontend"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("expected %s, got %s", want[i], got[i])
		}
	}
}

func TestNetworkNamesForProject_SkipsExternal(t *testing.T) {
	networks := map[string]compose.Network{
		"internal": {},
		"external": {External: true},
	}
	got := networkNamesForProject(networks, "myproject")
	for _, name := range got {
		if name == "myproject_external" {
			t.Errorf("external network should not be included in deletion list")
		}
	}
	if len(got) != 1 || got[0] != "myproject_internal" {
		t.Errorf("expected [myproject_internal], got %v", got)
	}
}

func TestNetworkNamesForProject_Empty(t *testing.T) {
	got := networkNamesForProject(nil, "myproject")
	if len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestVolumeNamesForProject(t *testing.T) {
	volumes := map[string]compose.Volume{
		"data":  {},
		"cache": {},
	}
	got := volumeNamesForProject(volumes, "myproject")
	sort.Strings(got)
	want := []string{"myproject_cache", "myproject_data"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("expected %s, got %s", want[i], got[i])
		}
	}
}
