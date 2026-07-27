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
	"time"

	"github.com/ieee0824/apricot/internal/compose"
	"github.com/ieee0824/apricot/internal/runner"
)

// supportsNetworks reports whether the current macOS version supports
// non-default network configuration (requires macOS 26+).
func supportsNetworks() bool {
	return supportsNetworksForVersion(macOSProductVersion())
}

// supportsNetworksForVersion reports whether the given macOS product version
// string supports non-default network configuration (requires macOS 26+).
// Empty or unparseable versions are treated as unsupported.
func supportsNetworksForVersion(v string) bool {
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

// validateScaleTargets returns the names present in scale that do not
// correspond to a defined service.
func validateScaleTargets(scale scaleMap, services map[string]compose.Service) []string {
	var unknown []string
	for name := range scale {
		if _, ok := services[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	return unknown
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
	composeDir, _ := filepath.Abs(filepath.Dir(*file))

	cf, err := compose.Load(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Warn about --scale targets that don't match any defined service.
	for _, name := range validateScaleTargets(scale, cf.Services) {
		fmt.Fprintf(os.Stderr, "Warning: --scale references unknown service %q; ignoring\n", name)
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

	// Reject services that reference networks not declared at the top level,
	// matching docker-compose (otherwise the container is started with a
	// network that was never created and fails with an opaque runtime error).
	if err := validateServiceNetworks(cf); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Create networks (skip external networks)
	for name, net := range cf.Networks {
		if net.External {
			continue
		}
		networkName := projectName + "_" + name
		if runner.NetworkExists(networkName) {
			continue
		}
		fmt.Printf("Creating network %s\n", networkName)
		if err := runner.NetworkCreate(buildNetworkCreateArgs(networkName, net)); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: network create failed for %s: %v\n", networkName, err)
		}
	}

	// Create volumes
	for name := range cf.Volumes {
		volumeName := projectName + "_" + name
		if runner.VolumeExists(volumeName) {
			continue
		}
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

	// Filter to only requested services (and their dependencies) if specified
	if targets := fs.Args(); len(targets) > 0 {
		needed := make(map[string]bool)
		var collect func(string)
		collect = func(name string) {
			if needed[name] {
				return
			}
			svc, ok := cf.Services[name]
			if !ok {
				return
			}
			needed[name] = true
			for _, dep := range compose.ToDependsOn(svc.DependsOn) {
				collect(dep)
			}
		}
		for _, t := range targets {
			if _, ok := cf.Services[t]; !ok {
				fmt.Fprintf(os.Stderr, "Error: no such service: %s\n", t)
				os.Exit(1)
			}
			collect(t)
		}
		filtered := make([]string, 0, len(order))
		for _, name := range order {
			if needed[name] {
				filtered = append(filtered, name)
			}
		}
		order = filtered
	}

	// Collect started container names for log streaming (foreground mode)
	type startedContainer struct {
		name    string
		service string
	}
	var started []startedContainer

	// Pre-compute normalized healthchecks per service for service_healthy waits.
	healthSpecs := map[string]compose.HealthcheckSpec{}
	for sname, s := range cf.Services {
		if spec, ok := s.Healthcheck.Normalize(); ok {
			healthSpecs[sname] = spec
		}
	}

	for _, name := range order {
		svc := cf.Services[name]

		// Wait for dependencies this service requires to be healthy. Services are
		// started in dependency order, so a dependency is already running here.
		for dep, cond := range compose.ToDependsOnConditions(svc.DependsOn) {
			if cond != "service_healthy" {
				continue
			}
			spec, ok := healthSpecs[dep]
			if !ok {
				fmt.Fprintf(os.Stderr, "Warning: service %q depends on %q being healthy, but %q has no healthcheck; not waiting\n", name, dep, dep)
				continue
			}
			for _, sc := range started {
				if sc.service != dep {
					continue
				}
				fmt.Printf("Waiting for %s to be healthy...\n", sc.name)
				if err := waitForHealthy(context.Background(), sc.name, spec, runner.ExecCheck); err != nil {
					fmt.Fprintf(os.Stderr, "Error: %v\n", err)
					os.Exit(1)
				}
			}
		}

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
			buildArgs, err := buildImageArgs(imageName, bc)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			if err := runner.Build(buildArgs); err != nil {
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
			if err := runner.StopQuiet(containerName); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to stop existing container %s: %v\n", containerName, err)
			}
			if err := runner.DeleteQuiet(containerName); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to delete existing container %s: %v\n", containerName, err)
			}

			if err := ensureBindMountDirs(compose.ToVolumeList(svc.Volumes), composeDir); err != nil {
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
		if err := runner.StopQuiet(s.name); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to stop %s: %v\n", s.name, err)
		}
	}
	wg.Wait()
}

// waitForHealthy polls a container's healthcheck until it passes, returning an
// error once the container is considered unhealthy. Failures during the start
// period do not count toward the retry budget, matching docker healthcheck
// semantics. The check function (runner.ExecCheck in production) is injected so
// the loop can be tested without a real container.
func waitForHealthy(ctx context.Context, container string, spec compose.HealthcheckSpec, check func(context.Context, string, []string) error) error {
	start := time.Now()
	failures := 0
	for {
		cctx, cancel := context.WithTimeout(ctx, spec.Timeout)
		err := check(cctx, container, spec.Cmd)
		cancel()
		if err == nil {
			return nil
		}
		if time.Since(start) >= spec.StartPeriod {
			failures++
			if failures >= spec.Retries {
				return fmt.Errorf("container %s did not become healthy after %d attempts", container, failures)
			}
		}
		select {
		case <-time.After(spec.Interval):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// buildRunArgs converts a Service to `container run` arguments (excluding -d and the command itself).
func buildRunArgs(containerName, serviceName, projectName, composeDir string, svc compose.Service, cf *compose.ComposeFile) []string {
	var args []string

	args = append(args, "--name", containerName)

	// Platform
	if svc.Platform != "" {
		args = append(args, "--platform", svc.Platform)
	}

	// Ports
	for _, p := range compose.ToPortList(svc.Ports) {
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
	for _, v := range compose.ToVolumeList(svc.Volumes) {
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
	for _, l := range labelSlice(svc.Labels) {
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

	// Init process (signal forwarding / zombie reaping)
	if svc.Init {
		args = append(args, "--init")
	}

	// Resource limits (ulimits)
	for _, u := range compose.ToUlimitSlice(svc.Ulimits) {
		args = append(args, "--ulimit", u)
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

	// Entrypoint: apple container's --entrypoint takes a single command, so for
	// exec-form entrypoints (["sh", "-c", ...]) only the first element is the
	// entrypoint; the rest are passed as container arguments below.
	entrypointParts := compose.ToStringSlice(svc.Entrypoint)
	if len(entrypointParts) > 0 {
		args = append(args, "--entrypoint", entrypointParts[0])
	}

	// Image
	args = append(args, svc.Image)

	// Command: remaining entrypoint args (exec form) come before the service
	// command, mirroring how an entrypoint array and command compose together.
	if len(entrypointParts) > 1 {
		args = append(args, entrypointParts[1:]...)
	}
	args = append(args, compose.ToStringSlice(svc.Command)...)

	return args
}

func labelSlice(v interface{}) []string {
	if labels := compose.ToStringSlice(v); labels != nil {
		return labels
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return nil
	}
	result := make([]string, 0, len(m))
	for k, value := range m {
		result = append(result, fmt.Sprintf("%s=%v", k, value))
	}
	return result
}

// buildImageArgs returns the args for `container build` (options + context).
func buildImageArgs(imageName string, bc *compose.BuildConfig) ([]string, error) {
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
			// Prevent relative dockerfile path from escaping the build context
			// by checking if the resolved path still starts with the context prefix.
			rel, err := filepath.Rel(filepath.Clean(ctx), filepath.Clean(df))
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				return nil, fmt.Errorf("dockerfile path %q escapes build context %q", bc.Dockerfile, ctx)
			}
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

	return args, nil
}

// ensureBindMountDirs creates host directories for bind mount volumes
// that don't exist yet. Named volumes (no path prefix) are skipped.
//
// Relative paths are resolved against composeDir and must stay within it, to
// prevent a compose file from escaping the project directory via "../". Absolute
// paths are the user's explicit choice: those inside composeDir are created,
// while those outside it (e.g. /etc/localtime or a shared cache) are left for
// the runtime to manage rather than being rejected.
func ensureBindMountDirs(volumes []string, composeDir string) error {
	baseReal := resolveExistingSymlinks(composeDir)
	for _, v := range volumes {
		hostPath := parseBindMountHostPath(v)
		if hostPath == "" {
			continue
		}
		resolved := filepath.Clean(hostPath)
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Clean(filepath.Join(composeDir, hostPath))
		}
		// Resolve symlinks before validating so a symlink inside the project
		// cannot point the real directory outside composeDir.
		real := resolveExistingSymlinks(resolved)
		if validatePathWithinBase(real, baseReal) != nil {
			if filepath.IsAbs(hostPath) {
				continue // absolute path outside the project; not ours to create
			}
			return fmt.Errorf("path %q escapes compose directory %q", resolved, composeDir)
		}
		if err := mkdirBindMount(resolved); err != nil {
			return err
		}
	}
	return nil
}

// mkdirBindMount creates a bind-mount host directory with restrictive perms.
func mkdirBindMount(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return fmt.Errorf("failed to create bind mount directory %q: %w", path, err)
	}
	return nil
}

// resolveExistingSymlinks resolves symlinks along the deepest existing prefix of
// path, re-appending the not-yet-created remainder. This reveals where a
// bind-mount directory would actually live before it is created.
func resolveExistingSymlinks(path string) string {
	path = filepath.Clean(path)
	rest := ""
	for {
		if real, err := filepath.EvalSymlinks(path); err == nil {
			if rest == "" {
				return real
			}
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(path)
		if parent == path {
			return filepath.Clean(filepath.Join(path, rest))
		}
		rest = filepath.Join(filepath.Base(path), rest)
		path = parent
	}
}

// validatePathWithinBase ensures that resolved is under baseDir.
// Returns an error when the path escapes the base directory.
func validatePathWithinBase(resolved, baseDir string) error {
	base := filepath.Clean(baseDir) + string(filepath.Separator)
	if resolved != filepath.Clean(baseDir) && !strings.HasPrefix(resolved, base) {
		return fmt.Errorf("path %q escapes compose directory %q", resolved, baseDir)
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
	// Normalize bare "." and ".." so they are recognized as relative bind-mount
	// paths rather than being passed through as volume names.
	host := normalizeBareDot(parts[0])
	if !strings.HasPrefix(host, "/") && !strings.HasPrefix(host, ".") && !strings.HasPrefix(host, "~") {
		return volume // named volume, leave as-is
	}
	host = expandTilde(host)
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
	host := normalizeBareDot(parts[0])
	// Named volumes don't start with / . or ~
	if !strings.HasPrefix(host, "/") && !strings.HasPrefix(host, ".") && !strings.HasPrefix(host, "~") {
		return ""
	}
	return expandTilde(host)
}

// normalizeBareDot rewrites a host path of exactly "." or ".." into host + "/"
// so it is unambiguously treated as a relative path. Other values are returned
// unchanged.
func normalizeBareDot(host string) string {
	if host == "." || host == ".." {
		return host + "/"
	}
	return host
}

// expandTilde expands a leading ~ or ~/ to the user's home directory. Other
// inputs (and ~user, which apricot does not resolve) are returned unchanged.
func expandTilde(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

// validateServiceNetworks ensures every network referenced by a service is
// declared in the top-level networks: section, matching docker-compose, which
// rejects references to undefined networks.
func validateServiceNetworks(cf *compose.ComposeFile) error {
	for name, svc := range cf.Services {
		for _, key := range compose.ToNetworkNames(svc.Networks) {
			if _, ok := cf.Networks[key]; !ok {
				return fmt.Errorf("service %q references undefined network %q", name, key)
			}
		}
	}
	return nil
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
