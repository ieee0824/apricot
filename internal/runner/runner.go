package runner

import (
	"encoding/json"
	"fmt"
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

// NetworkCreate creates a network.
func NetworkCreate(name string) error {
	cmd := exec.Command("container", "network", "create", name)
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
