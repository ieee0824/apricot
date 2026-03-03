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
	for _, c := range containersForProject(containers, projectName) {
		fmt.Printf("Stopping %s\n", c.Name)
		if err := runner.Stop(c.Name); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: stop failed for %s: %v\n", c.Name, err)
		}

		fmt.Printf("Removing %s\n", c.Name)
		if err := runner.Delete(c.Name); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: delete failed for %s: %v\n", c.Name, err)
		}
	}

	// Remove networks (after containers are gone)
	for _, networkName := range networkNamesForProject(cf.Networks, projectName) {
		fmt.Printf("Removing network %s\n", networkName)
		if err := runner.NetworkDelete(networkName); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: network delete failed for %s: %v\n", networkName, err)
		}
	}

	// Remove volumes if requested
	if *removeVolumes {
		for _, volumeName := range volumeNamesForProject(cf.Volumes, projectName) {
			fmt.Printf("Removing volume %s\n", volumeName)
			if err := runner.VolumeDelete(volumeName); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: volume delete failed for %s: %v\n", volumeName, err)
			}
		}
	}
}

// containersForProject returns containers whose name starts with "<projectName>-".
func containersForProject(containers []runner.Container, projectName string) []runner.Container {
	prefix := projectName + "-"
	var result []runner.Container
	for _, c := range containers {
		if strings.HasPrefix(c.Name, prefix) {
			result = append(result, c)
		}
	}
	return result
}

// networkNamesForProject returns the qualified network names to delete for a project.
// External networks are skipped since they are not managed by apricot.
func networkNamesForProject(networks map[string]compose.Network, projectName string) []string {
	result := make([]string, 0, len(networks))
	for name, net := range networks {
		if net.External {
			continue
		}
		result = append(result, projectName+"_"+name)
	}
	return result
}

// volumeNamesForProject returns the qualified volume names for a project.
func volumeNamesForProject(volumes map[string]compose.Volume, projectName string) []string {
	result := make([]string, 0, len(volumes))
	for name := range volumes {
		result = append(result, projectName+"_"+name)
	}
	return result
}
