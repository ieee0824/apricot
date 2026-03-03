package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	"github.com/ieee0824/apricot/internal/compose"
	"github.com/ieee0824/apricot/internal/runner"
)

func runUp(args []string) {
	fs := flag.NewFlagSet("up", flag.ExitOnError)
	detach := fs.Bool("d", false, "Run containers in background")
	file := fs.String("f", "docker-compose.yaml", "Path to docker-compose.yaml")
	project := fs.String("p", "", "Project name (default: current directory name)")
	fs.Parse(args)

	projectName := resolveProjectName(*project)

	cf, err := compose.Load(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Create networks (skip external networks)
	for name, net := range cf.Networks {
		if net.External {
			continue
		}
		networkName := projectName + "_" + name
		fmt.Printf("Creating network %s\n", networkName)
		if err := runner.NetworkCreate(buildNetworkCreateArgs(networkName, net)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: network create failed for %s: %v\n", networkName, err)
		}
	}

	// Create volumes
	for name := range cf.Volumes {
		volumeName := projectName + "_" + name
		fmt.Printf("Creating volume %s\n", volumeName)
		if err := runner.VolumeCreate(volumeName); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: volume create failed for %s: %v\n", volumeName, err)
		}
	}

	// Sort services by dependency order
	order, err := compose.SortServices(cf.Services)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	for _, name := range order {
		svc := cf.Services[name]

		// Build image if build: is defined
		if bc := compose.ToBuildConfig(svc.Build); bc != nil {
			imageName := svc.Image
			if imageName == "" {
				imageName = projectName + "_" + name
			}
			fmt.Printf("Building %s\n", imageName)
			if err := runner.Build(buildImageArgs(imageName, bc)); err != nil {
				fmt.Fprintf(os.Stderr, "Error building %s: %v\n", imageName, err)
				os.Exit(1)
			}
			// If image: was not set, use the built image name for run
			if svc.Image == "" {
				svc.Image = imageName
			}
		}

		containerName := containerNameFor(projectName, name, svc.ContainerName)
		args := buildRunArgs(containerName, name, projectName, svc, cf)

		fmt.Printf("Starting %s\n", containerName)
		if err := runner.Run(args, *detach); err != nil {
			fmt.Fprintf(os.Stderr, "Error starting %s: %v\n", containerName, err)
			os.Exit(1)
		}
	}
}

// buildRunArgs converts a Service to `container run` arguments (excluding -d and the command itself).
func buildRunArgs(containerName, serviceName, projectName string, svc compose.Service, cf *compose.ComposeFile) []string {
	var args []string

	args = append(args, "--name", containerName)

	// Ports
	for _, p := range svc.Ports {
		args = append(args, "-p", p)
	}

	// Environment
	for _, e := range compose.ToEnvSlice(svc.Environment) {
		args = append(args, "-e", e)
	}

	// Env files
	for _, f := range compose.ToStringSlice(svc.EnvFile) {
		args = append(args, "--env-file", f)
	}

	// Volumes
	for _, v := range svc.Volumes {
		args = append(args, "-v", v)
	}

	// Networks
	networkKeys := compose.ToNetworkNames(svc.Networks)
	if len(networkKeys) == 0 && len(cf.Networks) > 0 {
		// attach to all project networks if none specified
		for n := range cf.Networks {
			networkKeys = append(networkKeys, n)
		}
	}
	for _, key := range networkKeys {
		args = append(args, "--network", compose.ResolveNetworkName(key, projectName, cf.Networks[key]))
	}

	// Labels
	for _, l := range compose.ToStringSlice(svc.Labels) {
		args = append(args, "-l", l)
	}
	// Always add project label
	args = append(args, "-l", "apricot.project="+projectName)
	args = append(args, "-l", "apricot.service="+serviceName)

	// Working directory
	if svc.WorkingDir != "" {
		args = append(args, "-w", svc.WorkingDir)
	}

	// User
	if svc.User != "" {
		args = append(args, "-u", svc.User)
	}

	// CPUs
	if svc.CPUs > 0 {
		args = append(args, "-c", strconv.FormatFloat(svc.CPUs, 'f', -1, 64))
	}

	// Memory
	if svc.MemLimit != "" {
		args = append(args, "-m", svc.MemLimit)
	}

	// TTY / interactive
	if svc.Tty {
		args = append(args, "-t")
	}
	if svc.StdinOpen {
		args = append(args, "-i")
	}

	// Read-only
	if svc.ReadOnly {
		args = append(args, "--read-only")
	}

	// tmpfs
	for _, t := range compose.ToStringSlice(svc.Tmpfs) {
		args = append(args, "--tmpfs", t)
	}

	// DNS
	for _, d := range compose.ToStringSlice(svc.DNS) {
		args = append(args, "--dns", d)
	}

	// Entrypoint
	entrypointParts := compose.ToStringSlice(svc.Entrypoint)
	if len(entrypointParts) > 0 {
		args = append(args, "--entrypoint", entrypointParts[0])
	}

	// Image
	args = append(args, svc.Image)

	// Command (additional arguments after image)
	args = append(args, compose.ToStringSlice(svc.Command)...)

	return args
}

// buildImageArgs returns the args for `container build` (options + context).
func buildImageArgs(imageName string, bc *compose.BuildConfig) []string {
	var args []string

	args = append(args, "-t", imageName)

	if bc.Dockerfile != "" {
		args = append(args, "-f", bc.Dockerfile)
	}
	if bc.Target != "" {
		args = append(args, "--target", bc.Target)
	}
	if bc.NoCache {
		args = append(args, "--no-cache")
	}
	for k, v := range bc.Args {
		args = append(args, "--build-arg", k+"="+v)
	}
	for k, v := range bc.Labels {
		args = append(args, "-l", k+"="+v)
	}

	// Context directory (must be last)
	ctx := bc.Context
	if ctx == "" {
		ctx = "."
	}
	args = append(args, ctx)

	return args
}

// buildNetworkCreateArgs returns the args for `container network create` (options + name).
func buildNetworkCreateArgs(networkName string, net compose.Network) []string {
	var args []string
	if net.Internal {
		args = append(args, "--internal")
	}
	for k, v := range net.Labels {
		args = append(args, "--label", k+"="+v)
	}
	args = append(args, networkName)
	return args
}
