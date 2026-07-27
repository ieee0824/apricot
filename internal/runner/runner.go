package runner

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// execCommand and execCommandContext are package-level indirections over the
// os/exec constructors so tests can substitute a fake command runner without a
// real container runtime.
var (
	execCommand        = exec.Command
	execCommandContext = exec.CommandContext
)

// Container represents a container from `container list --format json`.
//
// The CLI emits a nested object: identity/image/labels live under
// "configuration", while the state is reported under "status". The exact shape
// of "status" changed between CLI versions (see parseState), so the JSON is
// decoded via UnmarshalJSON into the flat fields apricot actually uses. A plain
// struct with `json:"name"`-style tags would silently leave every field empty.
type Container struct {
	ID     string
	Name   string
	Image  string
	State  string
	Labels map[string]string
}

// containerJSON mirrors the relevant subset of the nested shape emitted by
// `container list --format json`. "status" is kept raw because its shape
// differs across CLI versions (a string in 0.8.x, an object in 1.0.x).
type containerJSON struct {
	Status        json.RawMessage `json:"status"`
	Configuration struct {
		ID     string            `json:"id"`
		Labels map[string]string `json:"labels"`
		Image  struct {
			Reference string `json:"reference"`
		} `json:"image"`
	} `json:"configuration"`
}

// UnmarshalJSON flattens the nested CLI output into Container.
func (c *Container) UnmarshalJSON(b []byte) error {
	var raw containerJSON
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	// The container's id doubles as its name/handle for stop/delete/logs.
	c.ID = raw.Configuration.ID
	c.Name = raw.Configuration.ID
	c.Image = raw.Configuration.Image.Reference
	c.State = parseState(raw.Status)
	c.Labels = raw.Configuration.Labels
	return nil
}

// parseState extracts the container state, tolerating both CLI shapes:
//   - 1.0.x: {"status": {"state": "stopped", ...}}
//   - 0.8.x: {"status": "stopped"}
func parseState(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var obj struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.State != "" {
		return obj.State
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

// Run executes `container run` with the given arguments.
// If detach is false, the command is attached to stdio.
func Run(args []string, detach bool) error {
	cmdArgs := []string{"run"}
	if detach {
		cmdArgs = append(cmdArgs, "-d")
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := execCommand("container", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if !detach {
		cmd.Stdin = os.Stdin
	}
	return cmd.Run()
}

// Stop stops the container with the given name/id.
func Stop(name string) error {
	cmd := execCommand("container", "stop", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// StopQuiet stops the container without printing output (for cleanup).
func StopQuiet(name string) error {
	return execCommand("container", "stop", name).Run()
}

// DeleteQuiet deletes the container without printing output (for cleanup).
func DeleteQuiet(name string) error {
	return execCommand("container", "delete", name).Run()
}

// Delete deletes the container with the given name/id.
func Delete(name string) error {
	cmd := execCommand("container", "delete", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// List returns all containers (including stopped ones) as a slice.
func List(all bool) ([]Container, error) {
	args := []string{"list", "--format", "json"}
	if all {
		args = append(args, "--all")
	}
	out, err := execCommand("container", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("container list failed: %w", err)
	}

	var containers []Container
	if err := json.Unmarshal(out, &containers); err != nil {
		return nil, fmt.Errorf("failed to parse container list output: %w", err)
	}
	return containers, nil
}

// LogsFollow streams logs from a container, writing each line with a prefix to w.
// Blocks until the context is cancelled or the container exits.
func LogsFollow(ctx context.Context, name, prefix string, w io.Writer) {
	cmd := execCommandContext(ctx, "container", "logs", "-f", name)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(w, "logs: failed to start for %s: %v\n", name, err)
		return
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			fmt.Fprintf(w, "%s | %s\n", prefix, scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			fmt.Fprintf(w, "logs: read error for %s: %v\n", name, err)
		}
	}()

	cmd.Wait()
	pw.Close()
	<-done
}

// Logs streams logs for a container.
func Logs(name string, follow bool) error {
	args := []string{"logs"}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, name)

	cmd := execCommand("container", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Build runs `container build` with the given args.
func Build(args []string) error {
	cmdArgs := append([]string{"build"}, args...)
	cmd := execCommand("container", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// NetworkCreate creates a network with the given args (options + name).
func NetworkCreate(args []string) error {
	cmdArgs := append([]string{"network", "create"}, args...)
	cmd := execCommand("container", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// NetworkExists reports whether the named network exists, based on the exit
// status of `container network inspect`.
func NetworkExists(name string) bool {
	cmd := execCommand("container", "network", "inspect", name)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

// VolumeExists reports whether the named volume exists, based on the exit
// status of `container volume inspect`.
func VolumeExists(name string) bool {
	cmd := execCommand("container", "volume", "inspect", name)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}

// VolumeCreate creates a volume.
func VolumeCreate(name string) error {
	cmd := execCommand("container", "volume", "create", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// VolumeDelete deletes a volume.
func VolumeDelete(name string) error {
	cmd := execCommand("container", "volume", "delete", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ExecCheck runs cmd inside the named container and returns nil only if it exits
// zero. Output is discarded; it is used for health probing. The supplied context
// bounds how long the probe may run.
func ExecCheck(ctx context.Context, container string, cmd []string) error {
	args := append([]string{"exec", container}, cmd...)
	return execCommandContext(ctx, "container", args...).Run()
}

// Exec runs `container exec` with the given args (options + container + command).
func Exec(args []string) error {
	cmdArgs := append([]string{"exec"}, args...)
	cmd := execCommand("container", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// NetworkDelete deletes a network.
func NetworkDelete(name string) error {
	cmd := execCommand("container", "network", "delete", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
