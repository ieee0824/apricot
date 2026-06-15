package main

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"strconv"
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
	prefix := projectName + "-"

	// When no service is given, restrict output to services in the compose file.
	var services map[string]compose.Service
	if len(serviceArgs) == 0 {
		cf, err := compose.Load(*file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		services = cf.Services
	}

	containers, err := runner.List(true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing containers: %v\n", err)
		os.Exit(1)
	}

	matched := false
	for _, c := range containers {
		if !strings.HasPrefix(c.Name, prefix) {
			continue
		}
		suffix := strings.TrimPrefix(c.Name, prefix)
		svcName := serviceNameFromSuffix(suffix, services, serviceArgs)
		if svcName == "" {
			continue
		}
		matched = true
		fmt.Printf("=== Logs for %s ===\n", c.Name)
		// Following multiple containers serially would block on the first, so
		// only honor -f when a single explicit service was requested.
		followThis := *follow && len(serviceArgs) == 1
		if err := runner.Logs(c.Name, followThis); err != nil {
			fmt.Fprintf(os.Stderr, "Error fetching logs for %s: %v\n", c.Name, err)
		}
	}

	if !matched && len(serviceArgs) > 0 {
		fmt.Fprintf(os.Stderr, "No containers found for: %s\n", strings.Join(serviceArgs, ", "))
	}
}

// serviceNameFromSuffix resolves the service a container belongs to from its
// name suffix (the part after "<project>-"). Scaled containers carry a trailing
// "-<index>" (e.g. "web-2"), so both the full suffix and the base name are
// considered. It returns the matched service name, or "" if the container
// should be skipped.
//
// When serviceArgs is non-empty, the container must belong to one of those
// services. Otherwise it must belong to a service defined in the compose file.
func serviceNameFromSuffix(suffix string, services map[string]compose.Service, serviceArgs []string) string {
	candidates := []string{suffix}
	if base := stripScaleIndex(suffix); base != suffix {
		candidates = append(candidates, base)
	}
	for _, name := range candidates {
		if len(serviceArgs) > 0 {
			if slices.Contains(serviceArgs, name) {
				return name
			}
			continue
		}
		if _, ok := services[name]; ok {
			return name
		}
	}
	return ""
}

// stripScaleIndex removes a trailing "-<digits>" suffix added for scaled
// services, so "web-2" becomes "web". Names without such a suffix are returned
// unchanged.
func stripScaleIndex(name string) string {
	i := strings.LastIndex(name, "-")
	if i < 0 {
		return name
	}
	if _, err := strconv.Atoi(name[i+1:]); err != nil {
		return name
	}
	return name[:i]
}
