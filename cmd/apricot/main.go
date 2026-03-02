package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "up":
		runUp(os.Args[2:])
	case "down":
		runDown(os.Args[2:])
	case "ps":
		runPs(os.Args[2:])
	case "logs":
		runLogs(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println(`apricot - docker compose compatible command for Apple Container

USAGE:
  apricot <command> [options]

COMMANDS:
  up      Start services defined in docker-compose.yaml
  down    Stop and remove services
  ps      List containers for the current project
  logs    Show logs for services

OPTIONS (common):
  -f <file>     Path to docker-compose.yaml (default: docker-compose.yaml)
  -p <project>  Project name (default: current directory name)

Run 'apricot <command> --help' for command-specific options.`)
}

// resolveProjectName returns the project name: explicit value or current dir name.
func resolveProjectName(explicit string) string {
	if explicit != "" {
		return explicit
	}
	dir, err := os.Getwd()
	if err != nil {
		return "apricot"
	}
	return filepath.Base(dir)
}

// containerNameFor returns the container name for a service.
// If the service has container_name set, use that; otherwise use <project>-<service>.
func containerNameFor(projectName, serviceName, containerName string) string {
	if containerName != "" {
		return containerName
	}
	return projectName + "-" + serviceName
}
