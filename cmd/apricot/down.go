package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ieee0824/apricot/internal/compose"
	"github.com/ieee0824/apricot/internal/runner"
)

func runDown(args []string) {
	fs := flag.NewFlagSet("down", flag.ExitOnError)
	file := fs.String("f", "docker-compose.yaml", "Path to docker-compose.yaml")
	project := fs.String("p", "", "Project name (default: current directory name)")
	removeVolumes := fs.Bool("v", false, "Remove named volumes")
	fs.Parse(args)

	projectName := resolveProjectName(*project)

	cf, err := compose.Load(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Get all containers (including stopped)
	containers, err := runner.List(true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing containers: %v\n", err)
		os.Exit(1)
	}

	// Stop and delete containers matching this project
	prefix := projectName + "-"
	for _, c := range containers {
		if !strings.HasPrefix(c.Name, prefix) {
			continue
		}

		// Verify it belongs to this project via label check if needed.
		// Simple prefix match is sufficient for now.
		fmt.Printf("Stopping %s\n", c.Name)
		if err := runner.Stop(c.Name); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: stop failed for %s: %v\n", c.Name, err)
		}

		fmt.Printf("Removing %s\n", c.Name)
		if err := runner.Delete(c.Name); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: delete failed for %s: %v\n", c.Name, err)
		}
	}

	// Remove volumes if requested
	if *removeVolumes {
		for name := range cf.Volumes {
			volumeName := projectName + "_" + name
			fmt.Printf("Removing volume %s\n", volumeName)
			if err := runner.VolumeDelete(volumeName); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: volume delete failed for %s: %v\n", volumeName, err)
			}
		}
	}
}
