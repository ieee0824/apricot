package main

import (
	"slices"
	"testing"

	"github.com/ieee0824/apricot/internal/compose"
)

func TestContainerNameFor(t *testing.T) {
	tests := []struct {
		project       string
		service       string
		containerName string
		want          string
	}{
		{"myproject", "web", "", "myproject-web"},
		{"myproject", "db", "custom-db", "custom-db"},
	}
	for _, tt := range tests {
		got := containerNameFor(tt.project, tt.service, tt.containerName)
		if got != tt.want {
			t.Errorf("containerNameFor(%q, %q, %q) = %q, want %q",
				tt.project, tt.service, tt.containerName, got, tt.want)
		}
	}
}

func TestBuildRunArgs_Basic(t *testing.T) {
	svc := compose.Service{
		Image: "nginx:latest",
		Ports: []string{"8080:80"},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("myproject-web", "web", "myproject", svc, cf)

	assertContainsSequence(t, args, "--name", "myproject-web")
	assertContainsSequence(t, args, "-p", "8080:80")
	assertContains(t, args, "nginx:latest")
	assertContainsSequence(t, args, "-l", "apricot.project=myproject")
	assertContainsSequence(t, args, "-l", "apricot.service=web")
}

func TestBuildRunArgs_Environment_Map(t *testing.T) {
	svc := compose.Service{
		Image:       "myapp",
		Environment: map[string]interface{}{"FOO": "bar"},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", svc, cf)

	assertContainsSequence(t, args, "-e", "FOO=bar")
}

func TestBuildRunArgs_Environment_Slice(t *testing.T) {
	svc := compose.Service{
		Image:       "myapp",
		Environment: []interface{}{"FOO=bar", "BAZ=qux"},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", svc, cf)

	assertContainsSequence(t, args, "-e", "FOO=bar")
	assertContainsSequence(t, args, "-e", "BAZ=qux")
}

func TestBuildRunArgs_Volumes(t *testing.T) {
	svc := compose.Service{
		Image:   "myapp",
		Volumes: []string{"./data:/data", "/tmp:/tmp"},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", svc, cf)

	assertContainsSequence(t, args, "-v", "./data:/data")
	assertContainsSequence(t, args, "-v", "/tmp:/tmp")
}

func TestBuildRunArgs_Network_Explicit(t *testing.T) {
	svc := compose.Service{
		Image:    "myapp",
		Networks: []interface{}{"frontend"},
	}
	cf := &compose.ComposeFile{
		Networks: map[string]compose.Network{"frontend": {}},
	}
	args := buildRunArgs("p-app", "app", "p", svc, cf)

	assertContainsSequence(t, args, "--network", "p_frontend")
}

func TestBuildRunArgs_Network_AutoAttach(t *testing.T) {
	// No networks on service but project has networks → auto-attach all
	svc := compose.Service{Image: "myapp"}
	cf := &compose.ComposeFile{
		Networks: map[string]compose.Network{"default": {}},
	}
	args := buildRunArgs("p-app", "app", "p", svc, cf)

	assertContainsSequence(t, args, "--network", "p_default")
}

func TestBuildRunArgs_WorkingDir(t *testing.T) {
	svc := compose.Service{Image: "myapp", WorkingDir: "/app"}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", svc, cf)

	assertContainsSequence(t, args, "-w", "/app")
}

func TestBuildRunArgs_User(t *testing.T) {
	svc := compose.Service{Image: "myapp", User: "1000:1000"}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", svc, cf)

	assertContainsSequence(t, args, "-u", "1000:1000")
}

func TestBuildRunArgs_Resources(t *testing.T) {
	svc := compose.Service{Image: "myapp", CPUs: 2.0, MemLimit: "512M"}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", svc, cf)

	assertContainsSequence(t, args, "-c", "2")
	assertContainsSequence(t, args, "-m", "512M")
}

func TestBuildRunArgs_Flags(t *testing.T) {
	svc := compose.Service{Image: "myapp", Tty: true, StdinOpen: true, ReadOnly: true}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", svc, cf)

	assertContains(t, args, "-t")
	assertContains(t, args, "-i")
	assertContains(t, args, "--read-only")
}

func TestBuildRunArgs_Entrypoint_And_Command(t *testing.T) {
	svc := compose.Service{
		Image:      "myapp",
		Entrypoint: "/entrypoint.sh",
		Command:    []interface{}{"arg1", "arg2"},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", svc, cf)

	assertContainsSequence(t, args, "--entrypoint", "/entrypoint.sh")
	// Command args come after image
	imgIdx := slices.Index(args, "myapp")
	if imgIdx == -1 {
		t.Fatal("image not found in args")
	}
	remaining := args[imgIdx+1:]
	if !slices.Contains(remaining, "arg1") || !slices.Contains(remaining, "arg2") {
		t.Errorf("command args not after image: %v", args)
	}
}

func TestBuildRunArgs_ImageIsLast_BeforeCommand(t *testing.T) {
	svc := compose.Service{
		Image:   "nginx:latest",
		Command: []interface{}{"nginx", "-g", "daemon off;"},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-web", "web", "p", svc, cf)

	imgIdx := slices.Index(args, "nginx:latest")
	if imgIdx == -1 {
		t.Fatal("image not found in args")
	}
	// All flags before image should start with "-" or "--"
	for _, a := range args[:imgIdx] {
		if a == "nginx:latest" {
			continue
		}
	}
	// Command args must be after image
	if args[imgIdx+1] != "nginx" {
		t.Errorf("expected 'nginx' after image, got %v", args[imgIdx:])
	}
}

func TestBuildImageArgs_Simple(t *testing.T) {
	bc := &compose.BuildConfig{Context: "./app"}
	args := buildImageArgs("myimage:latest", bc)
	assertContainsSequence(t, args, "-t", "myimage:latest")
	if args[len(args)-1] != "./app" {
		t.Errorf("context must be last arg, got %v", args)
	}
}

func TestBuildImageArgs_DefaultContext(t *testing.T) {
	bc := &compose.BuildConfig{}
	args := buildImageArgs("myimage", bc)
	if args[len(args)-1] != "." {
		t.Errorf("default context should be '.', got %v", args)
	}
}

func TestBuildImageArgs_Dockerfile(t *testing.T) {
	bc := &compose.BuildConfig{Context: ".", Dockerfile: "Dockerfile.dev"}
	args := buildImageArgs("myimage", bc)
	assertContainsSequence(t, args, "-f", "Dockerfile.dev")
}

func TestBuildImageArgs_Target(t *testing.T) {
	bc := &compose.BuildConfig{Context: ".", Target: "builder"}
	args := buildImageArgs("myimage", bc)
	assertContainsSequence(t, args, "--target", "builder")
}

func TestBuildImageArgs_NoCache(t *testing.T) {
	bc := &compose.BuildConfig{Context: ".", NoCache: true}
	args := buildImageArgs("myimage", bc)
	assertContains(t, args, "--no-cache")
}

func TestBuildImageArgs_BuildArgs(t *testing.T) {
	bc := &compose.BuildConfig{Context: ".", Args: map[string]string{"ENV": "prod"}}
	args := buildImageArgs("myimage", bc)
	assertContainsSequence(t, args, "--build-arg", "ENV=prod")
}

func TestBuildNetworkCreateArgs_Simple(t *testing.T) {
	net := compose.Network{}
	args := buildNetworkCreateArgs("myproject_frontend", net)
	last := args[len(args)-1]
	if last != "myproject_frontend" {
		t.Errorf("expected network name as last arg, got %q", last)
	}
	if slices.Contains(args, "--internal") {
		t.Errorf("--internal should not be present")
	}
}

func TestBuildNetworkCreateArgs_Internal(t *testing.T) {
	net := compose.Network{Internal: true}
	args := buildNetworkCreateArgs("myproject_backend", net)
	assertContains(t, args, "--internal")
	assertContains(t, args, "myproject_backend")
}

func TestBuildNetworkCreateArgs_Labels(t *testing.T) {
	net := compose.Network{Labels: map[string]string{"env": "prod"}}
	args := buildNetworkCreateArgs("myproject_net", net)
	assertContainsSequence(t, args, "--label", "env=prod")
	assertContains(t, args, "myproject_net")
}

func TestBuildNetworkCreateArgs_NetworkNameIsLast(t *testing.T) {
	net := compose.Network{Internal: true, Labels: map[string]string{"k": "v"}}
	args := buildNetworkCreateArgs("mynet", net)
	if args[len(args)-1] != "mynet" {
		t.Errorf("network name must be last arg, got %v", args)
	}
}

// helpers

func assertContains(t *testing.T, args []string, want string) {
	t.Helper()
	if !slices.Contains(args, want) {
		t.Errorf("expected %q in args %v", want, args)
	}
}

func assertContainsSequence(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i, a := range args {
		if a == flag && i+1 < len(args) && args[i+1] == value {
			return
		}
	}
	t.Errorf("expected sequence [%q %q] in args %v", flag, value, args)
}
