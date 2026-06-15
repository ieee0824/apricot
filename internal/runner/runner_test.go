package runner

import (
	"encoding/json"
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
