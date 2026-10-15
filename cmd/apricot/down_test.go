package main

import (
	"sort"
	"testing"

	"github.com/ieee0824/apricot/internal/compose"
	"github.com/ieee0824/apricot/internal/runner"
)

func TestContainersForProject(t *testing.T) {
	containers := []runner.Container{
		{Name: "myproject-web", Labels: map[string]string{"apricot.project": "myproject", "apricot.service": "web"}},
		{Name: "custom-db", Labels: map[string]string{"apricot.project": "myproject", "apricot.service": "db"}},
		{Name: "myproject-orphan", Labels: map[string]string{"apricot.project": "myproject", "apricot.service": "orphan"}},
		{Name: "other-web", Labels: map[string]string{"apricot.project": "other", "apricot.service": "web"}},
		{Name: "myproject-unlabeled"},
		{Name: "myproject-other", Labels: map[string]string{"apricot.project": "other", "apricot.service": "web"}},
		{Name: "myproject-no-service", Labels: map[string]string{"apricot.project": "myproject"}},
	}

	services := map[string]compose.Service{"web": {}, "db": {}}
	got := containersForProject(containers, "myproject", services)
	if len(got) != 2 {
		t.Fatalf("expected 2 owned containers, got %d: %v", len(got), got)
	}
	if got[0].Name != "myproject-web" || got[1].Name != "custom-db" {
		t.Errorf("unexpected containers selected: %v", got)
	}
}

func TestContainersForProject_Empty(t *testing.T) {
	got := containersForProject(nil, "myproject", nil)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestContainersForProject_OnlyCurrentComposeServices(t *testing.T) {
	containers := []runner.Container{
		{Name: "myproject-web", Labels: map[string]string{"apricot.project": "myproject", "apricot.service": "web"}},
		{Name: "custom-name", Labels: map[string]string{"apricot.project": "myproject", "apricot.service": "custom"}},
	}
	services := map[string]compose.Service{"custom": {}}
	got := containersForProject(containers, "myproject", services)
	if len(got) != 1 || got[0].Name != "custom-name" {
		t.Fatalf("expected only the service in the current Compose file, got %v", got)
	}
	if got := containersForProject(containers, "myproject", nil); len(got) != 0 {
		t.Errorf("expected no containers for a file without services, got %v", got)
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

func TestNetworkNamesForProject_ImplicitDefault(t *testing.T) {
	cf := &compose.ComposeFile{Services: map[string]compose.Service{"frontend": {}}, Networks: map[string]compose.Network{"private": {}}}
	addImplicitDefaultNetwork(cf)
	got := networkNamesForProject(cf.Networks, "project")
	sort.Strings(got)
	want := []string{"project_default", "project_private"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("network names = %v, want %v", got, want)
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
