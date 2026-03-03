package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ieee0824/apricot/internal/compose"
	"github.com/ieee0824/apricot/internal/runner"
)

func runExec(args []string) {
	fs := flag.NewFlagSet("exec", flag.ExitOnError)
	file := fs.String("file", "docker-compose.yaml", "Path to docker-compose.yaml")
	project := fs.String("p", "", "Project name (default: current directory name)")
	tty := fs.Bool("t", false, "Open a TTY")
	interactive := fs.Bool("i", false, "Keep stdin open")
	detach := fs.Bool("d", false, "Run detached")
	user := fs.String("u", "", "User (name|uid[:gid])")
	workdir := fs.String("w", "", "Working directory inside the container")
	fs.Parse(args)

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "Usage: apricot exec [options] <service> <command> [args...]")
		os.Exit(1)
	}

	serviceName := fs.Arg(0)
	cmdArgs := fs.Args()[1:]

	projectName := resolveProjectName(*project)

	cf, err := compose.Load(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	svc, ok := cf.Services[serviceName]
	if !ok {
		fmt.Fprintf(os.Stderr, "Error: service %q not found\n", serviceName)
		os.Exit(1)
	}

	containerName := containerNameFor(projectName, serviceName, svc.ContainerName)

	execArgs := buildExecArgs(containerName, cmdArgs, execOptions{
		tty:         *tty,
		interactive: *interactive,
		detach:      *detach,
		user:        *user,
		workdir:     *workdir,
	})

	if err := runner.Exec(execArgs); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

type execOptions struct {
	tty         bool
	interactive bool
	detach      bool
	user        string
	workdir     string
}

func buildExecArgs(containerName string, cmdArgs []string, opts execOptions) []string {
	var args []string

	if opts.tty {
		args = append(args, "-t")
	}
	if opts.interactive {
		args = append(args, "-i")
	}
	if opts.detach {
		args = append(args, "-d")
	}
	if opts.user != "" {
		args = append(args, "-u", opts.user)
	}
	if opts.workdir != "" {
		args = append(args, "-w", opts.workdir)
	}

	args = append(args, containerName)
	args = append(args, cmdArgs...)

	return args
}
