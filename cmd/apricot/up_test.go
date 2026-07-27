package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

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
	args := buildRunArgs("myproject-web", "web", "myproject", "", svc, cf)

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
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)

	assertContainsSequence(t, args, "-e", "FOO=bar")
}

func TestBuildRunArgs_Environment_Slice(t *testing.T) {
	svc := compose.Service{
		Image:       "myapp",
		Environment: []interface{}{"FOO=bar", "BAZ=qux"},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)

	assertContainsSequence(t, args, "-e", "FOO=bar")
	assertContainsSequence(t, args, "-e", "BAZ=qux")
}

func TestBuildRunArgs_Volumes(t *testing.T) {
	svc := compose.Service{
		Image:   "myapp",
		Volumes: []string{"/abs/data:/data", "/tmp:/tmp"},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)

	assertContainsSequence(t, args, "-v", "/abs/data:/data")
	assertContainsSequence(t, args, "-v", "/tmp:/tmp")
}

func TestBuildRunArgs_Ports_LongSyntax(t *testing.T) {
	svc := compose.Service{
		Image: "myapp",
		Ports: []interface{}{
			map[string]interface{}{"target": 80, "published": 8080, "protocol": "tcp"},
		},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)
	assertContainsSequence(t, args, "-p", "8080:80/tcp")
}

func TestBuildRunArgs_Volumes_LongSyntax(t *testing.T) {
	svc := compose.Service{
		Image: "myapp",
		Volumes: []interface{}{
			map[string]interface{}{"type": "bind", "source": "/abs/data", "target": "/data", "read_only": true},
		},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)
	assertContainsSequence(t, args, "-v", "/abs/data:/data:ro")
}

func TestBuildRunArgs_Platform(t *testing.T) {
	svc := compose.Service{Image: "myapp", Platform: "linux/arm64"}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)
	assertContainsSequence(t, args, "--platform", "linux/arm64")
}

func TestWaitForHealthy_BecomesHealthy(t *testing.T) {
	spec := compose.HealthcheckSpec{Cmd: []string{"true"}, Interval: time.Millisecond, Timeout: time.Second, Retries: 5}
	calls := 0
	check := func(ctx context.Context, container string, cmd []string) error {
		calls++
		if calls < 3 {
			return errors.New("not ready")
		}
		return nil // healthy on the 3rd attempt
	}
	if err := waitForHealthy(context.Background(), "c", spec, check); err != nil {
		t.Fatalf("expected healthy, got %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 checks, got %d", calls)
	}
}

func TestWaitForHealthy_GivesUpAfterRetries(t *testing.T) {
	spec := compose.HealthcheckSpec{Cmd: []string{"false"}, Interval: time.Millisecond, Timeout: time.Second, Retries: 3}
	calls := 0
	check := func(ctx context.Context, container string, cmd []string) error {
		calls++
		return errors.New("always failing")
	}
	err := waitForHealthy(context.Background(), "c", spec, check)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if calls != 3 {
		t.Errorf("expected 3 attempts, got %d", calls)
	}
}

func TestWaitForHealthy_StartPeriodFailuresDontCount(t *testing.T) {
	// During the start period, failures must not consume the retry budget.
	spec := compose.HealthcheckSpec{Cmd: []string{"x"}, Interval: time.Millisecond, Timeout: time.Second, Retries: 2, StartPeriod: 50 * time.Millisecond}
	start := time.Now()
	check := func(ctx context.Context, container string, cmd []string) error {
		if time.Since(start) < 50*time.Millisecond {
			return errors.New("still in start period")
		}
		return nil // healthy once past the start period
	}
	if err := waitForHealthy(context.Background(), "c", spec, check); err != nil {
		t.Fatalf("expected eventual healthy despite start-period failures, got %v", err)
	}
}

func TestBuildRunArgs_Network_Explicit(t *testing.T) {
	svc := compose.Service{
		Image:    "myapp",
		Networks: []interface{}{"frontend"},
	}
	cf := &compose.ComposeFile{
		Networks: map[string]compose.Network{"frontend": {}},
	}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)

	assertContainsSequence(t, args, "--network", "p_frontend")
}

func TestBuildRunArgs_Network_AutoAttach(t *testing.T) {
	// No networks on service but project has networks → auto-attach all
	svc := compose.Service{Image: "myapp"}
	cf := &compose.ComposeFile{
		Networks: map[string]compose.Network{"default": {}},
	}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)

	assertContainsSequence(t, args, "--network", "p_default")
}

func TestBuildRunArgs_WorkingDir(t *testing.T) {
	svc := compose.Service{Image: "myapp", WorkingDir: "/app"}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)

	assertContainsSequence(t, args, "-w", "/app")
}

func TestBuildRunArgs_User(t *testing.T) {
	svc := compose.Service{Image: "myapp", User: "1000:1000"}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)

	assertContainsSequence(t, args, "-u", "1000:1000")
}

func TestBuildRunArgs_Resources(t *testing.T) {
	svc := compose.Service{Image: "myapp", CPUs: 2.0, MemLimit: "512M"}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)

	assertContainsSequence(t, args, "-c", "2")
	assertContainsSequence(t, args, "-m", "512M")
}

func TestBuildRunArgs_Flags(t *testing.T) {
	svc := compose.Service{Image: "myapp", Tty: true, StdinOpen: true, ReadOnly: true}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)

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
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)

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
	args := buildRunArgs("p-web", "web", "p", "", svc, cf)

	imgIdx := slices.Index(args, "nginx:latest")
	if imgIdx == -1 {
		t.Fatal("image not found in args")
	}
	// Every arg before the image must be a flag (starting with "-") or a flag
	// value (preceded by a flag); none should be a bare positional.
	for i, a := range args[:imgIdx] {
		if !strings.HasPrefix(a, "-") {
			if i == 0 || !strings.HasPrefix(args[i-1], "-") {
				t.Errorf("arg %q at index %d before image is neither a flag nor a flag value: %v", a, i, args)
			}
		}
	}
	// Command args must be after image
	if args[imgIdx+1] != "nginx" {
		t.Errorf("expected 'nginx' after image, got %v", args[imgIdx:])
	}
}

func TestBuildImageArgs_Simple(t *testing.T) {
	bc := &compose.BuildConfig{Context: "./app"}
	args := mustBuildImageArgs(t, "myimage:latest", bc)
	assertContainsSequence(t, args, "-t", "myimage:latest")
	if args[len(args)-1] != "./app" {
		t.Errorf("context must be last arg, got %v", args)
	}
}

func TestBuildImageArgs_DefaultContext(t *testing.T) {
	bc := &compose.BuildConfig{}
	args := mustBuildImageArgs(t, "myimage", bc)
	if args[len(args)-1] != "." {
		t.Errorf("default context should be '.', got %v", args)
	}
}

func TestBuildImageArgs_Dockerfile(t *testing.T) {
	bc := &compose.BuildConfig{Context: ".", Dockerfile: "Dockerfile.dev"}
	args := mustBuildImageArgs(t, "myimage", bc)
	assertContainsSequence(t, args, "-f", "Dockerfile.dev")
}

func TestBuildImageArgs_Dockerfile_RelativeToContext(t *testing.T) {
	bc := &compose.BuildConfig{Context: "./container/mysql", Dockerfile: "Dockerfile"}
	args := mustBuildImageArgs(t, "myimage", bc)
	assertContainsSequence(t, args, "-f", "container/mysql/Dockerfile")
	if args[len(args)-1] != "./container/mysql" {
		t.Errorf("context must be last arg, got %v", args)
	}
}

func TestBuildImageArgs_Dockerfile_AbsolutePathNotJoined(t *testing.T) {
	bc := &compose.BuildConfig{Context: "./app", Dockerfile: "/opt/dockerfiles/Dockerfile.prod"}
	args := mustBuildImageArgs(t, "myimage", bc)
	assertContainsSequence(t, args, "-f", "/opt/dockerfiles/Dockerfile.prod")
}

func TestBuildImageArgs_Dockerfile_PathTraversal(t *testing.T) {
	bc := &compose.BuildConfig{Context: "./app", Dockerfile: "../../etc/Dockerfile.evil"}
	_, err := buildImageArgs("myimage", bc)
	if err == nil {
		t.Fatal("expected error for path traversal dockerfile, got nil")
	}
	if !strings.Contains(err.Error(), "escapes build context") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestBuildImageArgs_Dockerfile_DotDotPrefixDirAllowed(t *testing.T) {
	// A directory whose name merely starts with ".." (e.g. "..tools") is not a
	// parent-directory escape and must not be rejected.
	bc := &compose.BuildConfig{Context: ".", Dockerfile: "..tools/Dockerfile"}
	args, err := buildImageArgs("myimage", bc)
	if err != nil {
		t.Fatalf("dir name starting with .. should be allowed, got: %v", err)
	}
	assertContainsSequence(t, args, "-f", "..tools/Dockerfile")
}

func TestBuildImageArgs_Target(t *testing.T) {
	bc := &compose.BuildConfig{Context: ".", Target: "builder"}
	args := mustBuildImageArgs(t, "myimage", bc)
	assertContainsSequence(t, args, "--target", "builder")
}

func TestBuildImageArgs_NoCache(t *testing.T) {
	bc := &compose.BuildConfig{Context: ".", NoCache: true}
	args := mustBuildImageArgs(t, "myimage", bc)
	assertContains(t, args, "--no-cache")
}

func TestBuildImageArgs_BuildArgs(t *testing.T) {
	bc := &compose.BuildConfig{Context: ".", Args: map[string]string{"ENV": "prod"}}
	args := mustBuildImageArgs(t, "myimage", bc)
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

func TestBuildRunArgs_Init_Emitted(t *testing.T) {
	// apple container (v1.1.0+) supports --init, so init: true maps to it.
	svc := compose.Service{Image: "myapp", Init: true}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)
	assertContains(t, args, "--init")
}

func TestBuildRunArgs_Init_NotEmittedWhenUnset(t *testing.T) {
	svc := compose.Service{Image: "myapp"}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)
	assertNotContains(t, args, "--init")
}

func TestBuildRunArgs_DNSSearch(t *testing.T) {
	svc := compose.Service{Image: "myapp", DNSSearch: []interface{}{"example.com"}}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)
	assertContainsSequence(t, args, "--dns-search", "example.com")
}

func TestBuildRunArgs_DNSOpt(t *testing.T) {
	svc := compose.Service{Image: "myapp", DNSOpt: []interface{}{"ndots:2"}}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)
	assertContainsSequence(t, args, "--dns-option", "ndots:2")
}

func TestBuildRunArgs_Ulimits_Emitted(t *testing.T) {
	// apple container (v1.1.0+) supports --ulimit (format: <type>=<soft>[:<hard>]).
	svc := compose.Service{
		Image: "myapp",
		Ulimits: map[string]interface{}{
			"nofile": map[string]interface{}{"soft": 1024, "hard": 2048},
			"nproc":  512,
		},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)
	// Map iteration order is non-deterministic, so check each pair independently.
	assertContainsSequence(t, args, "--ulimit", "nofile=1024:2048")
	assertContainsSequence(t, args, "--ulimit", "nproc=512")
}

func TestBuildRunArgs_Ulimits_NotEmittedWhenUnset(t *testing.T) {
	svc := compose.Service{Image: "myapp"}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)
	assertNotContains(t, args, "--ulimit")
}

func TestRemoveFirstArg(t *testing.T) {
	// Only the first occurrence (the flag) is removed; a same-looking value
	// later in the container command survives.
	args := []string{"--name", "c", "-t", "-i", "myimg", "grep", "-i", "foo"}
	got := removeFirstArg(args, "-i")
	want := []string{"--name", "c", "-t", "myimg", "grep", "-i", "foo"}
	if !slices.Equal(got, want) {
		t.Errorf("removeFirstArg = %v, want %v", got, want)
	}
	if got := removeFirstArg([]string{"-t"}, "-i"); !slices.Equal(got, []string{"-t"}) {
		t.Errorf("removeFirstArg without match = %v, want unchanged", got)
	}
}

func TestBuildRunArgs_CapAddCapDrop_Emitted(t *testing.T) {
	// apple container (v0.12.0+) supports --cap-add / --cap-drop. Both prefixed
	// (CAP_NET_RAW) and unprefixed (NET_RAW) names are accepted, so values are
	// passed through unmodified.
	svc := compose.Service{
		Image:   "myapp",
		CapAdd:  []interface{}{"SYS_PTRACE", "CAP_NET_ADMIN"},
		CapDrop: []interface{}{"NET_RAW"},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)
	assertContainsSequence(t, args, "--cap-add", "SYS_PTRACE")
	assertContainsSequence(t, args, "--cap-add", "CAP_NET_ADMIN")
	assertContainsSequence(t, args, "--cap-drop", "NET_RAW")
}

func TestBuildRunArgs_CapAddCapDrop_NotEmittedWhenUnset(t *testing.T) {
	svc := compose.Service{Image: "myapp"}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)
	assertNotContains(t, args, "--cap-add")
	assertNotContains(t, args, "--cap-drop")
}

func TestBuildRunArgs_StringCommandIsShellSplit(t *testing.T) {
	// docker-compose splits string-form command with shlex; "sleep infinity"
	// must become two argv elements, not one "sleep infinity" executable.
	svc := compose.Service{Image: "myapp", Command: "sleep infinity"}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)
	assertContainsSequence(t, args, "sleep", "infinity")
	assertNotContains(t, args, "sleep infinity")
}

func TestBuildRunArgs_EntrypointExecForm(t *testing.T) {
	// Exec-form entrypoint: only the first element is the --entrypoint; the rest
	// must be preserved as container args after the image.
	svc := compose.Service{
		Image:      "myapp",
		Entrypoint: []interface{}{"sh", "-c", "echo hi"},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)

	assertContainsSequence(t, args, "--entrypoint", "sh")
	imgIdx := slices.Index(args, "myapp")
	if imgIdx == -1 {
		t.Fatal("image not found in args")
	}
	remaining := args[imgIdx+1:]
	if !slices.Equal(remaining, []string{"-c", "echo hi"}) {
		t.Errorf("entrypoint args not preserved after image: got %v", remaining)
	}
}

func TestScaleMap_Set_Valid(t *testing.T) {
	s := make(scaleMap)
	if err := s.Set("web=3"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s["web"] != 3 {
		t.Errorf("expected s[\"web\"]=3, got %d", s["web"])
	}
}

func TestScaleMap_Set_Multiple(t *testing.T) {
	s := make(scaleMap)
	s.Set("web=2")
	s.Set("db=1")
	if s["web"] != 2 || s["db"] != 1 {
		t.Errorf("unexpected scale map: %v", map[string]int(s))
	}
}

func TestScaleMap_Set_InvalidFormat(t *testing.T) {
	s := make(scaleMap)
	if err := s.Set("web"); err == nil {
		t.Error("expected error for missing =N")
	}
}

func TestScaleMap_Set_InvalidNumber(t *testing.T) {
	s := make(scaleMap)
	if err := s.Set("web=abc"); err == nil {
		t.Error("expected error for non-integer value")
	}
}

func TestScaleMap_Set_Zero(t *testing.T) {
	s := make(scaleMap)
	if err := s.Set("web=0"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s["web"] != 0 {
		t.Errorf("expected s[\"web\"]=0, got %d", s["web"])
	}
}

func TestParseMacOSMajorVersion(t *testing.T) {
	tests := []struct {
		input  string
		want   int
		wantOK bool
	}{
		{"15.3.1", 15, true},
		{"26.0", 26, true},
		{"26", 26, true},
		{"", 0, false},
		{"abc", 0, false},
	}
	for _, tt := range tests {
		got, ok := parseMacOSMajorVersion(tt.input)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("parseMacOSMajorVersion(%q) = (%d, %v), want (%d, %v)",
				tt.input, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestSupportsNetworksForVersion(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"26.0", true},
		{"25.4", false},
		{"", false},
		{"garbage", false},
	}
	for _, tt := range tests {
		if got := supportsNetworksForVersion(tt.input); got != tt.want {
			t.Errorf("supportsNetworksForVersion(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestValidateScaleTargets(t *testing.T) {
	services := map[string]compose.Service{"web": {}, "db": {}}
	scale := scaleMap{"web": 2, "worker": 3, "cache": 1}

	got := validateScaleTargets(scale, services)
	sort.Strings(got)
	if want := []string{"cache", "worker"}; !slices.Equal(got, want) {
		t.Errorf("validateScaleTargets() = %v, want %v", got, want)
	}

	if u := validateScaleTargets(scaleMap{"web": 1}, services); len(u) != 0 {
		t.Errorf("expected no unknown targets, got %v", u)
	}
}

func TestParseBindMountHostPath(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{".:/app", "./"},
		{"..:/app", "../"},
		{"./data:/data", "./data"},
		{"./.tmp/share:/share", "./.tmp/share"},
		{"/var/data:/data", "/var/data"},
		{"./data:/data:ro", "./data"},
		{"myvolume:/data", ""},         // named volume
		{"postgres_data:/var/lib", ""}, // named volume
		{"/data", ""},                  // container path only
	}
	for _, tt := range tests {
		got := parseBindMountHostPath(tt.input)
		if got != tt.want {
			t.Errorf("parseBindMountHostPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParseBindMountHostPath_TildeExpanded(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	if got, want := parseBindMountHostPath("~/data:/data"), filepath.Join(home, "data"); got != want {
		t.Errorf("parseBindMountHostPath(~/data:/data) = %q, want %q", got, want)
	}
	if got, want := parseBindMountHostPath("~:/data"), home; got != want {
		t.Errorf("parseBindMountHostPath(~:/data) = %q, want %q", got, want)
	}
}

func TestResolveVolumeHostPath_TildeExpanded(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	got := resolveVolumeHostPath("~/data:/data", "/project")
	want := filepath.Join(home, "data") + ":/data"
	if got != want {
		t.Errorf("resolveVolumeHostPath(~/data:/data) = %q, want %q", got, want)
	}
}

func TestEnsureBindMountDirs(t *testing.T) {
	dir := t.TempDir()
	volumes := []string{
		dir + "/sub/dir:/container/path",
		"namedvol:/data",
	}
	if err := ensureBindMountDirs(volumes, dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, err := os.Stat(dir + "/sub/dir")
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestEnsureBindMountDirs_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	volumes := []string{
		"../../../tmp/evil:/container/path",
	}
	err := ensureBindMountDirs(volumes, dir)
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
	if !strings.Contains(err.Error(), "escapes compose directory") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestEnsureBindMountDirs_AbsolutePathOutsideBaseAllowed(t *testing.T) {
	// Absolute host paths outside composeDir (e.g. /etc/localtime, a shared
	// cache) are legitimate and must not be rejected. apricot leaves them for
	// the runtime to manage, so no directory is created and no error is returned.
	base := t.TempDir()
	outside := filepath.Join(t.TempDir(), "shared-cache")
	volumes := []string{
		outside + ":/data",
	}
	if err := ensureBindMountDirs(volumes, base); err != nil {
		t.Fatalf("absolute path outside composeDir should be allowed, got: %v", err)
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Errorf("apricot should not create absolute paths outside composeDir; %q exists", outside)
	}
}

func TestEnsureBindMountDirs_SymlinkEscape(t *testing.T) {
	// A symlink inside the project pointing outside it must not let a bind mount
	// escape composeDir, even though the lexical path looks contained.
	base := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	err := ensureBindMountDirs([]string{"./link/sub:/data"}, base)
	if err == nil {
		t.Fatal("expected error for symlink escaping composeDir, got nil")
	}
	if !strings.Contains(err.Error(), "escapes compose directory") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestEnsureBindMountDirs_MkdirAllError(t *testing.T) {
	// A regular file where a directory is expected makes MkdirAll fail; the
	// error must be surfaced rather than swallowed.
	composeDir := t.TempDir()
	blocker := filepath.Join(composeDir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatalf("failed to create blocker file: %v", err)
	}
	if err := ensureBindMountDirs([]string{"./blocker/sub:/data"}, composeDir); err == nil {
		t.Fatal("expected error when MkdirAll is blocked by a file, got nil")
	}
}

func TestValidateServiceNetworks(t *testing.T) {
	// A service referencing an undefined network is rejected.
	cf := &compose.ComposeFile{
		Services: map[string]compose.Service{
			"web": {Networks: []interface{}{"frontend"}},
		},
		Networks: map[string]compose.Network{},
	}
	if err := validateServiceNetworks(cf); err == nil {
		t.Fatal("expected error for undefined network, got nil")
	}

	// Declaring the network makes it valid.
	cf.Networks["frontend"] = compose.Network{}
	if err := validateServiceNetworks(cf); err != nil {
		t.Errorf("declared network should validate, got: %v", err)
	}

	// A service with no networks is always valid.
	cf2 := &compose.ComposeFile{
		Services: map[string]compose.Service{"db": {}},
	}
	if err := validateServiceNetworks(cf2); err != nil {
		t.Errorf("service without networks should validate, got: %v", err)
	}
}

func TestValidatePathWithinBase(t *testing.T) {
	tests := []struct {
		resolved string
		base     string
		wantErr  bool
	}{
		{"/project/data", "/project", false},
		{"/project", "/project", false},
		{"/project/sub/deep", "/project", false},
		{"/other/place", "/project", true},
		{"/project/../etc", "/project", true},
		{"/tmp/evil", "/project", true},
	}
	for _, tt := range tests {
		err := validatePathWithinBase(filepath.Clean(tt.resolved), tt.base)
		if (err != nil) != tt.wantErr {
			t.Errorf("validatePathWithinBase(%q, %q) error=%v, wantErr=%v", tt.resolved, tt.base, err, tt.wantErr)
		}
	}
}

func TestResolveVolumeHostPath_Relative(t *testing.T) {
	got := resolveVolumeHostPath("./data:/data", "/project")
	if got != "/project/data:/data" {
		t.Errorf("got %q, want %q", got, "/project/data:/data")
	}
}

func TestResolveVolumeHostPath_Absolute(t *testing.T) {
	got := resolveVolumeHostPath("/abs/data:/data", "/project")
	if got != "/abs/data:/data" {
		t.Errorf("got %q, want %q", got, "/abs/data:/data")
	}
}

func TestResolveVolumeHostPath_BareCurrentDir(t *testing.T) {
	got := resolveVolumeHostPath(".:/app", "/project")
	if got != "/project:/app" {
		t.Errorf("got %q, want %q", got, "/project:/app")
	}
}

func TestResolveVolumeHostPath_BareParentDir(t *testing.T) {
	got := resolveVolumeHostPath("..:/app", "/project/sub")
	want := filepath.Join("/project/sub", "../") + ":/app"
	// filepath.Join normalizes "../" to parent
	if got != "/project:/app" {
		t.Errorf("got %q, want %q (or %q)", got, "/project:/app", want)
	}
}

func TestResolveVolumeHostPath_Named(t *testing.T) {
	got := resolveVolumeHostPath("myvolume:/data", "/project")
	if got != "myvolume:/data" {
		t.Errorf("named volume should be unchanged, got %q", got)
	}
}

func TestResolveVolumeHostPath_NoColon(t *testing.T) {
	got := resolveVolumeHostPath("/data", "/project")
	if got != "/data" {
		t.Errorf("got %q, want %q", got, "/data")
	}
}

func TestEnsureBindMountDirs_RelativePath(t *testing.T) {
	base := t.TempDir()
	volumes := []string{"./subdir:/container/path"}
	if err := ensureBindMountDirs(volumes, base); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, err := os.Stat(base + "/subdir")
	if err != nil {
		t.Fatalf("directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Error("expected directory")
	}
}

func TestResolveProjectName_Explicit(t *testing.T) {
	got := resolveProjectName("myproject")
	if got != "myproject" {
		t.Errorf("got %q, want %q", got, "myproject")
	}
}

func TestResolveProjectName_FromCwd(t *testing.T) {
	got := resolveProjectName("")
	if got == "" {
		t.Error("expected non-empty project name from cwd")
	}
}

func TestBuildRunArgs_EnvFile_AbsolutePath(t *testing.T) {
	svc := compose.Service{
		Image:   "myapp",
		EnvFile: []interface{}{"/abs/.env"},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "/some/dir", svc, cf)
	assertContainsSequence(t, args, "--env-file", "/abs/.env")
}

func TestBuildRunArgs_EnvFile_RelativePath(t *testing.T) {
	svc := compose.Service{
		Image:   "myapp",
		EnvFile: []interface{}{".env"},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "/project/dir", svc, cf)
	assertContainsSequence(t, args, "--env-file", "/project/dir/.env")
}

func TestBuildRunArgs_RelativeVolume(t *testing.T) {
	svc := compose.Service{
		Image:   "myapp",
		Volumes: []string{"./data:/data"},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "/project", svc, cf)
	assertContainsSequence(t, args, "-v", "/project/data:/data")
}

func TestBuildRunArgs_BareCurrentDirVolume(t *testing.T) {
	svc := compose.Service{
		Image:   "myapp",
		Volumes: []string{".:/app"},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "/project", svc, cf)
	assertContainsSequence(t, args, "-v", "/project:/app")
}

func TestBuildRunArgs_DotSlashVolume(t *testing.T) {
	svc := compose.Service{
		Image:   "myapp",
		Volumes: []string{"./:/app"},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "/project", svc, cf)
	assertContainsSequence(t, args, "-v", "/project:/app")
}

func TestBuildRunArgs_CPUs_Float(t *testing.T) {
	svc := compose.Service{Image: "myapp", CPUs: 0.5}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)
	assertContainsSequence(t, args, "-c", "1") // ceil(0.5) = 1
}

func TestBuildRunArgs_Labels_Map(t *testing.T) {
	svc := compose.Service{
		Image: "myapp",
		Labels: map[string]interface{}{
			"com.example.role": "worker",
			"com.example.tier": "backend",
		},
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)

	assertContainsSequence(t, args, "-l", "com.example.role=worker")
	assertContainsSequence(t, args, "-l", "com.example.tier=backend")
}

func TestBuildRunArgs_ExternalNetworkWithCustomName(t *testing.T) {
	svc := compose.Service{
		Image:    "myapp",
		Networks: []interface{}{"shared"},
	}
	cf := &compose.ComposeFile{
		Networks: map[string]compose.Network{
			"shared": {External: true, Name: "company-shared"},
		},
	}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)

	assertContainsSequence(t, args, "--network", "company-shared")
}

func TestBuildRunArgs_TmpfsAndDNS_StringForms(t *testing.T) {
	svc := compose.Service{
		Image:      "myapp",
		Tmpfs:      "/run",
		DNS:        "1.1.1.1",
		Entrypoint: "/bootstrap.sh",
	}
	cf := &compose.ComposeFile{}
	args := buildRunArgs("p-app", "app", "p", "", svc, cf)

	assertContainsSequence(t, args, "--tmpfs", "/run")
	assertContainsSequence(t, args, "--dns", "1.1.1.1")
	assertContainsSequence(t, args, "--entrypoint", "/bootstrap.sh")
}

func TestScaleMap_String(t *testing.T) {
	s := scaleMap{"web": 3, "db": 1}
	parts := strings.Split(s.String(), ",")
	sort.Strings(parts)
	want := []string{"db=1", "web=3"}
	if !slices.Equal(parts, want) {
		t.Fatalf("scaleMap.String() = %q, parts %v, want %v", s.String(), parts, want)
	}
}

func TestLabelSlice_Map(t *testing.T) {
	got := labelSlice(map[string]interface{}{
		"com.example.one": "1",
		"com.example.two": "2",
	})
	sort.Strings(got)
	want := []string{"com.example.one=1", "com.example.two=2"}
	if !slices.Equal(got, want) {
		t.Fatalf("labelSlice() = %v, want %v", got, want)
	}
}

// helpers

func mustBuildImageArgs(t *testing.T, imageName string, bc *compose.BuildConfig) []string {
	t.Helper()
	args, err := buildImageArgs(imageName, bc)
	if err != nil {
		t.Fatalf("buildImageArgs() unexpected error: %v", err)
	}
	return args
}

func assertContains(t *testing.T, args []string, want string) {
	t.Helper()
	if !slices.Contains(args, want) {
		t.Errorf("expected %q in args %v", want, args)
	}
}

func assertNotContains(t *testing.T, args []string, notWant string) {
	t.Helper()
	if slices.Contains(args, notWant) {
		t.Errorf("did not expect %q in args %v", notWant, args)
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
