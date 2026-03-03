package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/ieee0824/apricot/internal/compose"
	"github.com/ieee0824/apricot/internal/runner"
)

func runBuild(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	file := fs.String("f", "docker-compose.yaml", "Path to docker-compose.yaml")
	project := fs.String("p", "", "Project name (default: current directory name)")
	fs.Parse(args)

	projectName := resolveProjectName(*project)
	services := fs.Args() // optional: specific service names

	cf, err := compose.Load(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	order, err := compose.SortServices(cf.Services)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	for _, name := range order {
		if len(services) > 0 && !sliceContains(services, name) {
			continue
		}
		svc := cf.Services[name]
		bc := compose.ToBuildConfig(svc.Build)
		if bc == nil {
			continue
		}
		imageName := svc.Image
		if imageName == "" {
			imageName = projectName + "_" + name
		}
		fmt.Printf("Building %s\n", imageName)
		if err := runner.Build(buildImageArgs(imageName, bc)); err != nil {
			fmt.Fprintf(os.Stderr, "Error building %s: %v\n", imageName, err)
			os.Exit(1)
		}
	}
}

func sliceContains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
