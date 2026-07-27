package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ieee0824/apricot/internal/buildctx"
	"github.com/ieee0824/apricot/internal/compose"
	"github.com/ieee0824/apricot/internal/runner"
)

func runBuild(args []string) {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	file := fs.String("f", "docker-compose.yaml", "Path to docker-compose.yaml")
	project := fs.String("p", "", "Project name (default: current directory name)")
	fs.Parse(args)

	projectName := resolveProjectName(*project)
	composeDir := filepath.Dir(*file)
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
		if !filepath.IsAbs(bc.Context) {
			bc.Context = filepath.Join(composeDir, bc.Context)
		}
		imageName := svc.Image
		if imageName == "" {
			imageName = projectName + "_" + name
		}
		fmt.Printf("Building %s\n", imageName)
		cleanupCtx := prepareFilteredContext(bc)
		buildArgs, err := buildImageArgs(imageName, bc)
		if err != nil {
			cleanupCtx()
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		err = runner.Build(buildArgs)
		cleanupCtx()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error building %s: %v\n", imageName, err)
			os.Exit(1)
		}
	}
}

// prepareFilteredContext swaps bc.Context for a temporary copy that contains
// only the files .dockerignore lets through, so `container build` does not
// spend minutes walking excluded trees (apple/container#2026). It returns a
// cleanup function that removes the copy; when filtering is skipped or fails
// the original context is used and the returned cleanup is a no-op.
func prepareFilteredContext(bc *compose.BuildConfig) func() {
	tmp, cleanup, err := buildctx.Prepare(bc.Context, bc.Dockerfile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: build context filtering failed, building from original context: %v\n", err)
		return func() {}
	}
	if tmp == "" {
		return func() {}
	}
	bc.Context = tmp
	return cleanup
}

func sliceContains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
