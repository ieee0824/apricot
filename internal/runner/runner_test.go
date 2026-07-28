package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// realListJSON is a representative sample of `container list --all --format json`
// output from the apple container CLI. Each entry is nested under
// "configuration" and reports state as top-level "status". This fixture guards
// against regressing to a flat struct that silently parses every field to "".
const realListJSON = `[
  {
    "networks": [],
    "status": "stopped",
    "configuration": {
      "id": "myapp-redis",
      "labels": {"apricot.project": "myapp", "apricot.service": "redis"},
      "image": {"reference": "docker.io/library/redis:6"}
    }
  },
  {
    "networks": [],
    "status": "running",
    "configuration": {
      "id": "myapp-web",
      "labels": {"apricot.project": "myapp", "apricot.service": "web"},
      "image": {"reference": "nginx:latest"}
    }
  }
]`

func TestContainerUnmarshalNestedJSON(t *testing.T) {
	var got []Container
	if err := json.Unmarshal([]byte(realListJSON), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := []Container{
		{ID: "myapp-redis", Name: "myapp-redis", Image: "docker.io/library/redis:6", State: "stopped"},
		{ID: "myapp-web", Name: "myapp-web", Image: "nginx:latest", State: "running"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d containers, want %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.ID != w.ID || g.Name != w.Name || g.Image != w.Image || g.State != w.State {
			t.Errorf("container[%d] = {ID:%q Name:%q Image:%q State:%q}, want {ID:%q Name:%q Image:%q State:%q}",
				i, g.ID, g.Name, g.Image, g.State, w.ID, w.Name, w.Image, w.State)
		}
	}
}

// TestContainerUnmarshalParsesLabels ensures labels survive decoding; they
// identify the owning project/service and back project matching.
func TestContainerUnmarshalParsesLabels(t *testing.T) {
	var got []Container
	if err := json.Unmarshal([]byte(realListJSON), &got); err != nil {
		t.Fatal(err)
	}
	if got[0].Labels["apricot.project"] != "myapp" || got[0].Labels["apricot.service"] != "redis" {
		t.Errorf("labels not parsed: %v", got[0].Labels)
	}
}

// TestContainerUnmarshalPopulatesName is a focused guard against the original
// bug: a flat struct against the nested CLI output left Name empty, so name
// prefix matching in ps/down/logs matched nothing.
func TestContainerUnmarshalPopulatesName(t *testing.T) {
	var got []Container
	if err := json.Unmarshal([]byte(realListJSON), &got); err != nil {
		t.Fatal(err)
	}
	for i, c := range got {
		if c.Name == "" {
			t.Errorf("container[%d].Name is empty — nested JSON not parsed (flat-struct regression)", i)
		}
	}
}

// TestContainerUnmarshalEdgeCases covers degenerate CLI payloads: an entry
// without a "configuration" object, null labels, and an empty top-level array.
// None of these should panic and the missing fields should decode to zero
// values.
func TestContainerUnmarshalEdgeCases(t *testing.T) {
	t.Run("missing configuration", func(t *testing.T) {
		var got []Container
		if err := json.Unmarshal([]byte(`[{"status":"running"}]`), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d containers, want 1", len(got))
		}
		c := got[0]
		if c.ID != "" || c.Name != "" || c.Image != "" {
			t.Errorf("expected empty id/name/image, got {ID:%q Name:%q Image:%q}", c.ID, c.Name, c.Image)
		}
		if c.State != "running" {
			t.Errorf("State = %q, want %q", c.State, "running")
		}
		if c.Labels != nil {
			t.Errorf("Labels = %v, want nil", c.Labels)
		}
	})

	t.Run("null labels", func(t *testing.T) {
		var got []Container
		raw := `[{"status":"running","configuration":{"id":"x","labels":null,"image":{"reference":"img"}}}]`
		if err := json.Unmarshal([]byte(raw), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("got %d containers, want 1", len(got))
		}
		if got[0].Labels != nil {
			t.Errorf("Labels = %v, want nil", got[0].Labels)
		}
		if got[0].ID != "x" || got[0].Image != "img" {
			t.Errorf("unexpected fields: %+v", got[0])
		}
	})

	t.Run("empty array", func(t *testing.T) {
		var got []Container
		if err := json.Unmarshal([]byte(`[]`), &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("got %d containers, want 0", len(got))
		}
	})
}

// listJSONv1 is a representative `container list --format json` sample from CLI
// 1.0.x, where "status" is an object ({state, networks}) instead of the 0.8.x
// plain string. Identity/image/labels still live under "configuration".
const listJSONv1 = `[
  {
    "id": "myapp-redis",
    "configuration": {
      "id": "myapp-redis",
      "labels": {"apricot.project": "myapp", "apricot.service": "redis"},
      "image": {"reference": "docker.io/library/redis:6"}
    },
    "status": {"state": "running", "networks": []}
  }
]`

func TestContainerUnmarshal_V1StatusObject(t *testing.T) {
	var got []Container
	if err := json.Unmarshal([]byte(listJSONv1), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d containers, want 1", len(got))
	}
	c := got[0]
	if c.Name != "myapp-redis" || c.Image != "docker.io/library/redis:6" || c.State != "running" {
		t.Errorf("v1 parse = {Name:%q Image:%q State:%q}, want redis/running", c.Name, c.Image, c.State)
	}
	if c.Labels["apricot.project"] != "myapp" {
		t.Errorf("labels not parsed: %v", c.Labels)
	}
}

func TestParseState(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"v1 object", `{"state":"running","networks":[]}`, "running"},
		{"v0 string", `"stopped"`, "stopped"},
		{"empty object", `{}`, ""},
		{"empty string", `""`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseState([]byte(tt.raw)); got != tt.want {
				t.Errorf("parseState(%s) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
	if got := parseState(nil); got != "" {
		t.Errorf("parseState(nil) = %q, want empty", got)
	}
}

// fakeExecCommand returns a function suitable for assigning to execCommand. The
// returned commands re-invoke the test binary so it runs TestHelperProcess,
// which emits the JSON supplied via stdout and records the argv it received.
func fakeExecCommand(stdout string, exitCode int) (func(string, ...string) *exec.Cmd, *[]string) {
	var recorded []string
	fake := func(name string, args ...string) *exec.Cmd {
		recorded = append([]string{name}, args...)
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = []string{
			"GO_WANT_HELPER_PROCESS=1",
			"HELPER_STDOUT=" + stdout,
			"HELPER_EXIT_CODE=" + strconv.Itoa(exitCode),
		}
		return cmd
	}
	return fake, &recorded
}

// TestHelperProcess is not a real test; it's the child process spawned by
// fakeExecCommand. It writes HELPER_STDOUT to stdout and exits with
// HELPER_EXIT_CODE.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	os.Stdout.WriteString(os.Getenv("HELPER_STDOUT"))
	code := 0
	switch os.Getenv("HELPER_EXIT_CODE") {
	case "", "0":
		code = 0
	default:
		code = 1
	}
	os.Exit(code)
}

// withExecCommand temporarily overrides the package-level execCommand and
// restores it when the returned function is called.
func withExecCommand(fake func(string, ...string) *exec.Cmd) func() {
	orig := execCommand
	execCommand = fake
	return func() { execCommand = orig }
}

func TestListArgsAndParse(t *testing.T) {
	t.Run("all=true passes --all and parses nested JSON", func(t *testing.T) {
		fake, recorded := fakeExecCommand(realListJSON, 0)
		restore := withExecCommand(fake)
		defer restore()

		got, err := List(true)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		wantArgs := []string{"container", "list", "--format", "json", "--all"}
		if !reflect.DeepEqual(*recorded, wantArgs) {
			t.Errorf("argv = %v, want %v", *recorded, wantArgs)
		}
		if len(got) != 2 {
			t.Fatalf("got %d containers, want 2", len(got))
		}
		if got[0].Name != "myapp-redis" || got[1].Name != "myapp-web" {
			t.Errorf("unexpected names: %q, %q", got[0].Name, got[1].Name)
		}
	})

	t.Run("all=false omits --all", func(t *testing.T) {
		fake, recorded := fakeExecCommand(realListJSON, 0)
		restore := withExecCommand(fake)
		defer restore()

		if _, err := List(false); err != nil {
			t.Fatalf("List: %v", err)
		}
		wantArgs := []string{"container", "list", "--format", "json"}
		if !reflect.DeepEqual(*recorded, wantArgs) {
			t.Errorf("argv = %v, want %v", *recorded, wantArgs)
		}
	})

	t.Run("empty array yields empty slice", func(t *testing.T) {
		fake, _ := fakeExecCommand(`[]`, 0)
		restore := withExecCommand(fake)
		defer restore()

		got, err := List(false)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 0 {
			t.Errorf("got %d containers, want 0", len(got))
		}
	})

	t.Run("exec failure surfaces list error", func(t *testing.T) {
		fake, _ := fakeExecCommand("", 1)
		restore := withExecCommand(fake)
		defer restore()

		_, err := List(false)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "container list failed") {
			t.Errorf("error = %q, want it to contain %q", err.Error(), "container list failed")
		}
	})

	t.Run("malformed JSON surfaces parse error", func(t *testing.T) {
		fake, _ := fakeExecCommand(`{not json`, 0)
		restore := withExecCommand(fake)
		defer restore()

		_, err := List(false)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "failed to parse") {
			t.Errorf("error = %q, want it to contain %q", err.Error(), "failed to parse")
		}
	})
}

func TestCommandArgv(t *testing.T) {
	t.Run("Stop", func(t *testing.T) {
		fake, recorded := fakeExecCommand("", 0)
		restore := withExecCommand(fake)
		defer restore()

		if err := Stop("svc-web"); err != nil {
			t.Fatalf("Stop: %v", err)
		}
		want := []string{"container", "stop", "svc-web"}
		if !reflect.DeepEqual(*recorded, want) {
			t.Errorf("argv = %v, want %v", *recorded, want)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		fake, recorded := fakeExecCommand("", 0)
		restore := withExecCommand(fake)
		defer restore()

		if err := Delete("svc-web"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		want := []string{"container", "delete", "svc-web"}
		if !reflect.DeepEqual(*recorded, want) {
			t.Errorf("argv = %v, want %v", *recorded, want)
		}
	})

	t.Run("Run detached", func(t *testing.T) {
		fake, recorded := fakeExecCommand("", 0)
		restore := withExecCommand(fake)
		defer restore()

		if err := Run([]string{"--name", "svc-web", "nginx"}, true); err != nil {
			t.Fatalf("Run: %v", err)
		}
		want := []string{"container", "run", "-d", "--name", "svc-web", "nginx"}
		if !reflect.DeepEqual(*recorded, want) {
			t.Errorf("argv = %v, want %v", *recorded, want)
		}
	})

	t.Run("Run attached", func(t *testing.T) {
		fake, recorded := fakeExecCommand("", 0)
		restore := withExecCommand(fake)
		defer restore()

		if err := Run([]string{"nginx"}, false); err != nil {
			t.Fatalf("Run: %v", err)
		}
		want := []string{"container", "run", "nginx"}
		if !reflect.DeepEqual(*recorded, want) {
			t.Errorf("argv = %v, want %v", *recorded, want)
		}
	})

	cases := []struct {
		name string
		call func() error
		want []string
	}{
		{"StopQuiet", func() error { return StopQuiet("c") }, []string{"container", "stop", "c"}},
		{"DeleteQuiet", func() error { return DeleteQuiet("c") }, []string{"container", "delete", "c"}},
		{"Logs", func() error { return Logs("c", false) }, []string{"container", "logs", "c"}},
		{"Logs follow", func() error { return Logs("c", true) }, []string{"container", "logs", "-f", "c"}},
		{"Build", func() error { return Build([]string{"-t", "img", "."}) }, []string{"container", "build", "-t", "img", "."}},
		{"NetworkCreate", func() error { return NetworkCreate([]string{"--internal", "net"}) }, []string{"container", "network", "create", "--internal", "net"}},
		{"NetworkDelete", func() error { return NetworkDelete("net") }, []string{"container", "network", "delete", "net"}},
		{"VolumeCreate", func() error { return VolumeCreate("v") }, []string{"container", "volume", "create", "v"}},
		{"VolumeDelete", func() error { return VolumeDelete("v") }, []string{"container", "volume", "delete", "v"}},
		{"Exec", func() error { return Exec([]string{"c", "sh"}) }, []string{"container", "exec", "c", "sh"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake, recorded := fakeExecCommand("", 0)
			restore := withExecCommand(fake)
			defer restore()

			if err := tc.call(); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if !reflect.DeepEqual(*recorded, tc.want) {
				t.Errorf("argv = %v, want %v", *recorded, tc.want)
			}
		})
	}
}

func TestVolumeInitFromImage(t *testing.T) {
	fake, recorded := fakeExecCommand("", 0)
	restore := withExecCommand(fake)
	defer restore()

	if err := VolumeInitFromImage("p_data", "/var/lib/data", "myimg"); err != nil {
		t.Fatalf("VolumeInitFromImage: %v", err)
	}
	argv := *recorded
	wantPrefix := []string{"container", "run", "--rm", "-u", "0:0", "--entrypoint", "/bin/sh", "-v", "p_data:/.apricot-volume-init", "myimg", "-c"}
	if len(argv) != len(wantPrefix)+1 {
		t.Fatalf("argv = %v, want %d elements", argv, len(wantPrefix)+1)
	}
	for i, w := range wantPrefix {
		if argv[i] != w {
			t.Errorf("argv[%d] = %q, want %q", i, argv[i], w)
		}
	}
	script := argv[len(argv)-1]
	for _, must := range []string{"'/var/lib/data'", "chown", "chmod", "cp -a"} {
		if !strings.Contains(script, must) {
			t.Errorf("script missing %q:\n%s", must, script)
		}
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("/plain/path"); got != "'/plain/path'" {
		t.Errorf("shellQuote plain = %q", got)
	}
	if got := shellQuote("a'b"); got != `'a'"'"'b'` {
		t.Errorf("shellQuote quote = %q", got)
	}
}

func TestExists(t *testing.T) {
	cases := []struct {
		name     string
		call     func() bool
		exitCode int
		want     bool
		wantArgv []string
	}{
		{"ContainerExists existing", func() bool { return ContainerExists("c") }, 0, true, []string{"container", "inspect", "c"}},
		{"ContainerExists missing", func() bool { return ContainerExists("c") }, 1, false, []string{"container", "inspect", "c"}},
		{"ImageExists existing", func() bool { return ImageExists("img") }, 0, true, []string{"container", "image", "inspect", "img"}},
		{"ImageExists missing", func() bool { return ImageExists("img") }, 1, false, []string{"container", "image", "inspect", "img"}},
		{"NetworkExists existing", func() bool { return NetworkExists("net") }, 0, true, []string{"container", "network", "inspect", "net"}},
		{"NetworkExists missing", func() bool { return NetworkExists("net") }, 1, false, []string{"container", "network", "inspect", "net"}},
		{"VolumeExists existing", func() bool { return VolumeExists("v") }, 0, true, []string{"container", "volume", "inspect", "v"}},
		{"VolumeExists missing", func() bool { return VolumeExists("v") }, 1, false, []string{"container", "volume", "inspect", "v"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake, recorded := fakeExecCommand("", tc.exitCode)
			restore := withExecCommand(fake)
			defer restore()

			if got := tc.call(); got != tc.want {
				t.Errorf("exists = %v, want %v", got, tc.want)
			}
			if !reflect.DeepEqual(*recorded, tc.wantArgv) {
				t.Errorf("argv = %v, want %v", *recorded, tc.wantArgv)
			}
		})
	}
}

// fakeExecCommandContext mirrors fakeExecCommand for the CommandContext variant
// used by LogsFollow.
func fakeExecCommandContext(stdout string) func(context.Context, string, ...string) *exec.Cmd {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = []string{
			"GO_WANT_HELPER_PROCESS=1",
			"HELPER_STDOUT=" + stdout,
			"HELPER_EXIT_CODE=0",
		}
		return cmd
	}
}

func withExecCommandContext(fake func(context.Context, string, ...string) *exec.Cmd) func() {
	orig := execCommandContext
	execCommandContext = fake
	return func() { execCommandContext = orig }
}

func TestExecCheck(t *testing.T) {
	t.Run("exit 0 returns nil with correct argv", func(t *testing.T) {
		var recorded []string
		restore := withExecCommandContext(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			recorded = append([]string{name}, args...)
			cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "HELPER_EXIT_CODE=0"}
			return cmd
		})
		defer restore()

		if err := ExecCheck(context.Background(), "myctr", []string{"pg_isready", "-U", "me"}); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
		want := []string{"container", "exec", "myctr", "pg_isready", "-U", "me"}
		if !reflect.DeepEqual(recorded, want) {
			t.Errorf("argv = %v, want %v", recorded, want)
		}
	})

	t.Run("non-zero exit returns error", func(t *testing.T) {
		restore := withExecCommandContext(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = []string{"GO_WANT_HELPER_PROCESS=1", "HELPER_EXIT_CODE=1"}
			return cmd
		})
		defer restore()

		if err := ExecCheck(context.Background(), "myctr", []string{"false"}); err == nil {
			t.Fatal("expected error for non-zero exit")
		}
	})
}

func TestLogsFollow(t *testing.T) {
	t.Run("streams prefixed lines", func(t *testing.T) {
		restore := withExecCommandContext(fakeExecCommandContext("line1\nline2\n"))
		defer restore()

		var buf bytes.Buffer
		LogsFollow(context.Background(), "svc-web", "web", &buf)
		out := buf.String()
		if !strings.Contains(out, "web | line1") || !strings.Contains(out, "web | line2") {
			t.Errorf("unexpected logs output: %q", out)
		}
	})

	t.Run("reports start failure", func(t *testing.T) {
		restore := withExecCommandContext(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "/nonexistent/apricot-test-binary-xyz")
		})
		defer restore()

		var buf bytes.Buffer
		LogsFollow(context.Background(), "svc-web", "web", &buf)
		if !strings.Contains(buf.String(), "failed to start") {
			t.Errorf("expected start-failure message, got %q", buf.String())
		}
	})
}
