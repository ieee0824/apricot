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

// Container represents a running container from `container list --format json`.
type Container struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Image string `json:"image"`
	State string `json:"state"`
}

// Run executes `container run` with the given arguments.
// If detach is false, the command is attached to stdio.
func Run(args []string, detach bool) error {
	cmdArgs := []string{"run"}
	if detach {
		cmdArgs = append(cmdArgs, "-d")
	}
	cmdArgs = append(cmdArgs, args...)

	cmd := exec.Command("container", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if !detach {
		cmd.Stdin = os.Stdin
	}
	return cmd.Run()
}

// Stop stops the container with the given name/id.
func Stop(name string) error {
	cmd := exec.Command("container", "stop", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// StopQuiet stops the container without printing output (for cleanup).
func StopQuiet(name string) error {
	return exec.Command("container", "stop", name).Run()
}

// DeleteQuiet deletes the container without printing output (for cleanup).
func DeleteQuiet(name string) error {
	return exec.Command("container", "delete", name).Run()
}

// Delete deletes the container with the given name/id.
func Delete(name string) error {
	cmd := exec.Command("container", "delete", name)
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
	out, err := exec.Command("container", args...).Output()
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
	cmd := exec.CommandContext(ctx, "container", "logs", "-f", name)
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw

	if err := cmd.Start(); err != nil {
		return
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(pr)
		for scanner.Scan() {
			fmt.Fprintf(w, "%s | %s\n", prefix, scanner.Text())
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

	cmd := exec.Command("container", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Build runs `container build` with the given args.
func Build(args []string) error {
	cmdArgs := append([]string{"build"}, args...)
	cmd := exec.Command("container", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// NetworkCreate creates a network with the given args (options + name).
func NetworkCreate(args []string) error {
	cmdArgs := append([]string{"network", "create"}, args...)
	cmd := exec.Command("container", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// VolumeCreate creates a volume.
func VolumeCreate(name string) error {
	cmd := exec.Command("container", "volume", "create", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// VolumeDelete deletes a volume.
func VolumeDelete(name string) error {
	cmd := exec.Command("container", "volume", "delete", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Exec runs `container exec` with the given args (options + container + command).
func Exec(args []string) error {
	cmdArgs := append([]string{"exec"}, args...)
	cmd := exec.Command("container", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// NetworkDelete deletes a network.
func NetworkDelete(name string) error {
	cmd := exec.Command("container", "network", "delete", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
