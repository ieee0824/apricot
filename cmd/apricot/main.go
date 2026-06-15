package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
)

var (
	version   = ""
	buildTime = ""
)

func init() {
	if version != "" && buildTime != "" {
		return // both set via ldflags
	}
	info, ok := debug.ReadBuildInfo()
	version, buildTime = resolveVersionInfo(version, buildTime, info, ok)
}

// resolveVersionInfo computes the effective version and build time from any
// ldflags-provided values (ldVersion/ldBuildTime) and the embedded build info.
// ok reports whether build info was available. Missing pieces fall back to
// "dev" for the version and the vcs.time setting for the build time.
func resolveVersionInfo(ldVersion, ldBuildTime string, info *debug.BuildInfo, ok bool) (version, buildTime string) {
	version, buildTime = ldVersion, ldBuildTime
	if !ok {
		if version == "" {
			version = "dev"
		}
		return version, buildTime
	}
	if version == "" {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		} else {
			version = "dev"
		}
	}
	if buildTime == "" {
		for _, s := range info.Settings {
			if s.Key == "vcs.time" {
				buildTime = s.Value
				break
			}
		}
	}
	return version, buildTime
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "up":
		runUp(os.Args[2:])
	case "build":
		runBuild(os.Args[2:])
	case "down":
		runDown(os.Args[2:])
	case "ps":
		runPs(os.Args[2:])
	case "logs":
		runLogs(os.Args[2:])
	case "exec":
		runExec(os.Args[2:])
	case "version", "--version", "-v":
		if buildTime != "" {
			fmt.Printf("apricot %s (built %s)\n", version, buildTime)
		} else {
			fmt.Println("apricot", version)
		}
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
  up       Start services defined in docker-compose.yaml
  build    Build images defined in docker-compose.yaml
  down     Stop and remove services
  ps       List containers for the current project
  logs     Show logs for services
  exec     Run a command in a running service container
  version  Show version

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
