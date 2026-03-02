package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ieee0824/apricot/internal/compose"
	"github.com/ieee0824/apricot/internal/runner"
)

func runLogs(args []string) {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	file := fs.String("file", "docker-compose.yaml", "Path to docker-compose.yaml")
	project := fs.String("p", "", "Project name (default: current directory name)")
	follow := fs.Bool("follow", false, "Follow log output")
	fs.BoolVar(follow, "f", false, "Follow log output (shorthand)")
	fs.Parse(args)

	projectName := resolveProjectName(*project)
	serviceArgs := fs.Args() // remaining positional args are service names

	if len(serviceArgs) > 0 {
		// Show logs for specified services
		for _, svc := range serviceArgs {
			containerName := projectName + "-" + svc
			if err := runner.Logs(containerName, *follow); err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching logs for %s: %v\n", containerName, err)
			}
		}
		return
	}

	// No service specified: show logs for all services in the compose file
	cf, err := compose.Load(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	prefix := projectName + "-"
	containers, err := runner.List(true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing containers: %v\n", err)
		os.Exit(1)
	}

	for _, c := range containers {
		if !strings.HasPrefix(c.Name, prefix) {
			continue
		}
		// Check if this container belongs to a known service
		serviceName := strings.TrimPrefix(c.Name, prefix)
		if _, ok := cf.Services[serviceName]; !ok {
			continue
		}
		fmt.Printf("=== Logs for %s ===\n", c.Name)
		if err := runner.Logs(c.Name, false); err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching logs for %s: %v\n", c.Name, err)
		}
	}
}
