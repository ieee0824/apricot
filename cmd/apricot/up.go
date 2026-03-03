package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/ieee0824/apricot/internal/compose"
	"github.com/ieee0824/apricot/internal/runner"
)

// supportsNetworks reports whether the current macOS version supports
// non-default network configuration (requires macOS 26+).
func supportsNetworks() bool {
	v := macOSProductVersion()
	if v == "" {
		return false
	}
	major, ok := parseMacOSMajorVersion(v)
	if !ok {
		return false
	}
	return major >= 26
}

// parseMacOSMajorVersion extracts the major version number from a
// macOS product version string like "15.3.1".
func parseMacOSMajorVersion(version string) (int, bool) {
	parts := strings.SplitN(version, ".", 2)
	if len(parts) == 0 {
		return 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, false
	}
	return major, true
}

// scaleMap holds per-service scale counts, populated via repeated --scale flags.
type scaleMap map[string]int

func (s scaleMap) String() string {
	parts := make([]string, 0, len(s))
	for k, v := range s {
		parts = append(parts, fmt.Sprintf("%s=%d", k, v))
	}
	return strings.Join(parts, ",")
}

func (s scaleMap) Set(v string) error {
	parts := strings.SplitN(v, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid scale format %q, expected service=N", v)
	}
	n, err := strconv.Atoi(parts[1])
	if err != nil || n < 0 {
		return fmt.Errorf("invalid scale value %q: must be a non-negative integer", parts[1])
	}
	s[parts[0]] = n
	return nil
}

func runUp(args []string) {
	fs := flag.NewFlagSet("up", flag.ExitOnError)
	detach := fs.Bool("d", false, "Run containers in background")
	file := fs.String("f", "docker-compose.yaml", "Path to docker-compose.yaml")
	project := fs.String("p", "", "Project name (default: current directory name)")
	scale := make(scaleMap)
	fs.Var(scale, "scale", "Scale a service (format: service=N, repeatable)")
	fs.Parse(args)

	projectName := resolveProjectName(*project)
	composeDir := filepath.Dir(*file)

	cf, err := compose.Load(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Skip networks on macOS < 26 (Apple Container limitation)
	if !supportsNetworks() {
		if len(cf.Networks) > 0 {
			fmt.Fprintln(os.Stderr, "Warning: network configuration requires macOS 26 or newer, skipping networks")
		}
		cf.Networks = nil
		for name, svc := range cf.Services {
			svc.Networks = nil
			cf.Services[name] = svc
		}
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

	// Collect started container names for log streaming (foreground mode)
	type startedContainer struct {
		name    string
		service string
	}
	var started []startedContainer

	for _, name := range order {
		svc := cf.Services[name]

		// Build image if build: is defined
		if bc := compose.ToBuildConfig(svc.Build); bc != nil {
			// Resolve build context relative to the compose file's directory
			if !filepath.IsAbs(bc.Context) {
				bc.Context = filepath.Join(composeDir, bc.Context)
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
			if svc.Image == "" {
				svc.Image = imageName
			}
		}

		n, scaled := scale[name]
		if !scaled {
			n = 1
		}

		for i := 1; i <= n; i++ {
			var containerName string
			if scaled {
				containerName = projectName + "-" + name + "-" + strconv.Itoa(i)
			} else {
				containerName = containerNameFor(projectName, name, svc.ContainerName)
			}
			_ = runner.StopQuiet(containerName)
			_ = runner.DeleteQuiet(containerName)

			if err := ensureBindMountDirs(svc.Volumes, composeDir); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			runArgs := buildRunArgs(containerName, name, projectName, composeDir, svc, cf)

			fmt.Printf("Starting %s\n", containerName)
			// Always start detached; foreground mode streams logs below
			if err := runner.Run(runArgs, true); err != nil {
				fmt.Fprintf(os.Stderr, "Error starting %s: %v\n", containerName, err)
				os.Exit(1)
			}
			started = append(started, startedContainer{name: containerName, service: name})
		}
	}

	if *detach || len(started) == 0 {
		return
	}

	// Foreground mode: stream logs from all containers, stop on Ctrl+C
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Calculate prefix width for alignment
	maxLen := 0
	for _, s := range started {
		if len(s.service) > maxLen {
			maxLen = len(s.service)
		}
	}

	var wg sync.WaitGroup
	for _, s := range started {
		wg.Add(1)
		prefix := fmt.Sprintf("%-*s", maxLen, s.service)
		go func(containerName, pfx string) {
			defer wg.Done()
			runner.LogsFollow(ctx, containerName, pfx, os.Stdout)
		}(s.name, prefix)
	}

	<-sigCh
	fmt.Println("\nStopping...")
	cancel()
	for _, s := range started {
		_ = runner.StopQuiet(s.name)
	}
	wg.Wait()
}

// buildRunArgs converts a Service to `container run` arguments (excluding -d and the command itself).
func buildRunArgs(containerName, serviceName, projectName, composeDir string, svc compose.Service, cf *compose.ComposeFile) []string {
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
		if !filepath.IsAbs(f) {
			f = filepath.Join(composeDir, f)
		}
		args = append(args, "--env-file", f)
	}

	// Volumes
	for _, v := range svc.Volumes {
		v = resolveVolumeHostPath(v, composeDir)
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

	// CPUs (container run -c requires integer; round up from float)
	if svc.CPUs > 0 {
		args = append(args, "-c", strconv.Itoa(int(math.Ceil(svc.CPUs))))
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

	// DNS search
	for _, d := range compose.ToStringSlice(svc.DNSSearch) {
		args = append(args, "--dns-search", d)
	}

	// DNS options
	for _, d := range compose.ToStringSlice(svc.DNSOpt) {
		args = append(args, "--dns-option", d)
	}

	// Init
	if svc.Init {
		args = append(args, "--init")
	}

	// Ulimits
	for _, u := range compose.ToUlimitSlice(svc.Ulimits) {
		args = append(args, "--ulimit", u)
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

	// Resolve context early so dockerfile can be joined with it.
	ctx := bc.Context
	if ctx == "" {
		ctx = "."
	}

	args = append(args, "-t", imageName)

	if bc.Dockerfile != "" {
		// In docker-compose, dockerfile is relative to the build context.
		// Resolve it against ctx so `container build -f` finds the right file.
		df := bc.Dockerfile
		if !filepath.IsAbs(df) {
			df = filepath.Join(ctx, df)
		}
		args = append(args, "-f", df)
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
	args = append(args, ctx)

	return args
}

// ensureBindMountDirs creates host directories for bind mount volumes
// that don't exist yet. Named volumes (no path prefix) are skipped.
func ensureBindMountDirs(volumes []string, composeDir string) error {
	for _, v := range volumes {
		hostPath := parseBindMountHostPath(v)
		if hostPath == "" {
			continue
		}
		if !filepath.IsAbs(hostPath) {
			hostPath = filepath.Join(composeDir, hostPath)
		}
		if err := os.MkdirAll(hostPath, 0755); err != nil {
			return fmt.Errorf("failed to create bind mount directory %q: %w", hostPath, err)
		}
	}
	return nil
}

// resolveVolumeHostPath rewrites a volume spec's host path to be absolute,
// resolving relative paths against composeDir.
func resolveVolumeHostPath(volume, composeDir string) string {
	parts := strings.SplitN(volume, ":", 2)
	if len(parts) < 2 {
		return volume
	}
	host := parts[0]
	if !strings.HasPrefix(host, "/") && !strings.HasPrefix(host, ".") && !strings.HasPrefix(host, "~") {
		return volume // named volume, leave as-is
	}
	if !filepath.IsAbs(host) {
		host = filepath.Join(composeDir, host)
	}
	return host + ":" + parts[1]
}

// parseBindMountHostPath extracts the host path from a volume spec like
// "host:container[:opts]". Returns "" for named volumes.
func parseBindMountHostPath(volume string) string {
	parts := strings.SplitN(volume, ":", 2)
	if len(parts) < 2 {
		return ""
	}
	host := parts[0]
	// Named volumes don't start with / . or ~
	if !strings.HasPrefix(host, "/") && !strings.HasPrefix(host, ".") && !strings.HasPrefix(host, "~") {
		return ""
	}
	return host
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
