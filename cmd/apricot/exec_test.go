package main

import (
	"testing"
)

func TestBuildExecArgs_Basic(t *testing.T) {
	args := buildExecArgs("myproject-web", []string{"sh"}, execOptions{})

	assertContains(t, args, "myproject-web")
	assertContains(t, args, "sh")
	// container name must come before command
	containerIdx := indexOf(args, "myproject-web")
	cmdIdx := indexOf(args, "sh")
	if containerIdx == -1 || cmdIdx == -1 {
		t.Fatal("container name or command not found")
	}
	if containerIdx > cmdIdx {
		t.Errorf("container name must precede command: %v", args)
	}
}

func TestBuildExecArgs_Flags(t *testing.T) {
	args := buildExecArgs("myproject-web", []string{"bash"}, execOptions{
		tty:         true,
		interactive: true,
	})

	assertContains(t, args, "-t")
	assertContains(t, args, "-i")
}

func TestBuildExecArgs_Detach(t *testing.T) {
	args := buildExecArgs("myproject-web", []string{"sh", "-c", "echo hi"}, execOptions{
		detach: true,
	})

	assertContains(t, args, "-d")
}

func TestBuildExecArgs_User(t *testing.T) {
	args := buildExecArgs("myproject-web", []string{"whoami"}, execOptions{
		user: "1000:1000",
	})

	assertContainsSequence(t, args, "-u", "1000:1000")
}

func TestBuildExecArgs_Workdir(t *testing.T) {
	args := buildExecArgs("myproject-web", []string{"pwd"}, execOptions{
		workdir: "/app",
	})

	assertContainsSequence(t, args, "-w", "/app")
}

func TestBuildExecArgs_MultipleCommandArgs(t *testing.T) {
	args := buildExecArgs("p-app", []string{"sh", "-c", "echo hello"}, execOptions{})

	containerIdx := indexOf(args, "p-app")
	if containerIdx == -1 {
		t.Fatal("container name not found")
	}
	remaining := args[containerIdx+1:]
	if len(remaining) != 3 || remaining[0] != "sh" || remaining[1] != "-c" || remaining[2] != "echo hello" {
		t.Errorf("expected [sh -c echo hello] after container name, got %v", remaining)
	}
}

func TestBuildExecArgs_NoFlagsWhenNotSet(t *testing.T) {
	args := buildExecArgs("myproject-web", []string{"ls"}, execOptions{})

	for _, flag := range []string{"-t", "-i", "-d"} {
		if contains(args, flag) {
			t.Errorf("unexpected flag %q in args %v", flag, args)
		}
	}
}

// helpers

func indexOf(args []string, s string) int {
	for i, a := range args {
		if a == s {
			return i
		}
	}
	return -1
}

func contains(args []string, s string) bool {
	return indexOf(args, s) != -1
}
