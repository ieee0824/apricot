package runner

import (
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
}
