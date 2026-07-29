# apricot

[![Test](https://github.com/ieee0824/apricot/actions/workflows/test.yml/badge.svg)](https://github.com/ieee0824/apricot/actions/workflows/test.yml)
[![codecov](https://codecov.io/gh/ieee0824/apricot/branch/main/graph/badge.svg)](https://codecov.io/gh/ieee0824/apricot)


docker-compose like tool for [apple container](https://github.com/apple/container).

[日本語](README.ja.md)

## Goal

Load `docker-compose.yaml` as-is.

## Installation

### Homebrew (recommended)

```bash
brew tap ieee0824/tap
brew install apricot
```

### go install

```bash
go install github.com/ieee0824/apricot/cmd/apricot@latest
```

### Build from source

```bash
go build -o /usr/local/bin/apricot ./cmd/apricot/
```

## Usage

Run in the directory containing `docker-compose.yaml`.

### up

Start services.

```bash
apricot up                        # foreground
apricot up -d                     # background
apricot up --build                # force rebuild of build: services
apricot up --scale web=3          # start 3 instances of web
apricot up --scale web=3 --scale db=2  # scale multiple services
apricot up -f path/to/docker-compose.yaml  # specify file
apricot up -p myproject                    # specify project name
```

Scaled containers are named `<project>-<service>-<index>` (e.g. `myapp-web-1`, `myapp-web-2`).

Services with `build:` are built only when their image does not exist yet (same as docker-compose); pass `--build` to force a rebuild, or use `apricot build`.

### build

Build images defined in `docker-compose.yaml`.

```bash
apricot build           # build all services
apricot build web       # build specific service
```

### down

Stop and remove services.

```bash
apricot down        # stop and remove containers
apricot down -v     # also remove volumes
```

### ps

List containers for the current project.

```bash
apricot ps
apricot ps -a       # include stopped containers
```

### logs

Show container logs.

```bash
apricot logs              # all services
apricot logs web          # specific service
apricot logs -f web       # follow
```

### exec

Run a command in a running service container.

```bash
apricot exec web sh             # start sh
apricot exec -it web bash       # interactive + TTY
apricot exec -u 1000 web whoami # specify user
apricot exec -w /app web pwd    # specify working directory
```

| Option | Description |
|---|---|
| `-t` | Allocate TTY |
| `-i` | Keep stdin open |
| `-d` | Detached mode |
| `-u <user>` | Specify user |
| `-w <dir>` | Specify working directory |

## Global Options

| Option | Description | Default |
|---|---|---|
| `-f <file>` | Path to docker-compose.yaml | `docker-compose.yaml` |
| `-p <project>` | Project name | current directory name |

## Supported docker-compose.yaml Fields

| Field | Supported |
|---|---|
| `image` | ✅ |
| `build` | ✅ |
| `ports` | ✅ (short and long syntax) |
| `volumes` | ✅ (short and long syntax) |
| `environment` | ✅ |
| `env_file` | ✅ |
| `working_dir` | ✅ |
| `user` | ✅ |
| `entrypoint` | ✅ |
| `command` | ✅ |
| `platform` | ✅ |
| `networks` | ⚠️ (macOS 26+) |
| `labels` | ✅ |
| `cpus` | ✅ |
| `mem_limit` | ✅ |
| `stdin_open` | ✅ |
| `tty` | ✅ |
| `read_only` | ✅ |
| `tmpfs` | ✅ |
| `dns` | ✅ |
| `dns_search` | ✅ |
| `dns_opt` | ✅ |
| `init` | ✅ (maps to `container run --init`) |
| `ulimits` | ✅ (maps to `container run --ulimit`) |
| `cap_add` | ✅ (maps to `container run --cap-add`) |
| `cap_drop` | ✅ (maps to `container run --cap-drop`) |
| `depends_on` | ✅ (startup order + `condition: service_healthy`) |
| `healthcheck` | ✅ (used for `service_healthy` waits) |
| `container_name` | ✅ |
| `restart` | ❌ (not supported) |
| `security_opt` | ❌ (Apple Container has no equivalent) |

## Limitations

- **networks**: Non-default network configuration requires macOS 26 or newer (Apple Container runtime limitation). On older macOS versions, `networks` settings are automatically skipped with a warning.
- **Service discovery**: Apple Container has no container-to-container DNS on its networks ([apple/container#1809](https://github.com/apple/container/issues/1809)), so containers cannot resolve each other by name out of the box. apricot emulates docker-compose service discovery by appending entries mapping each service name and container name to its IP to `/etc/hosts` of every container on a shared network during `up`. Services started earlier (`depends_on` order) are resolvable from later ones at startup; later ones become resolvable from earlier ones as soon as they start. With `--scale N`, the bare service name points at the first replica; other replicas are reachable by container name (`<project>-<service>-<n>`). Injection needs `/bin/sh` and a writable `/etc/hosts` in the image (fails with a warning otherwise) and can be disabled with `APRICOT_DISABLE_HOSTS_INJECT=1`. Note that entries are not updated if a container is restarted outside of `apricot up` and gets a new IP.
- **init**: `init: true` is passed through as `container run --init`, which runs an init process that forwards signals and reaps zombie processes (Apple Container v1.1.0+).
- **ulimits**: Both the shorthand (`nofile: 1024`) and long form (`nofile: {soft: 1024, hard: 2048}`) are passed through as `container run --ulimit <type>=<soft>[:<hard>]` (Apple Container v1.1.0+).
- **cap_add / cap_drop**: Passed through as `container run --cap-add` / `--cap-drop` (Apple Container v0.12.0+). Both prefixed (`CAP_NET_RAW`) and unprefixed (`NET_RAW`) capability names work, as does `ALL`.
- **security_opt**: The Apple Container CLI has no `--security-opt` equivalent (seccomp/AppArmor profiles do not apply to its VM-per-container isolation model), so this setting is ignored with a warning.
- **healthcheck**: Apple Container has no native healthcheck, so apricot runs the `test` command inside the container via `container exec` (honoring `interval` / `timeout` / `retries` / `start_period`). It is used to satisfy `depends_on: { x: { condition: service_healthy } }`, which makes `up` wait for a dependency to become healthy before starting dependents. `condition: service_completed_successfully` is not yet supported.
- **Unsupported keys**: Any service key apricot does not handle (e.g. `deploy`, `restart`, `extends`, `profiles`) is reported with a warning when the compose file is loaded, instead of being silently dropped.
- **Named volumes**: Named volumes are project-scoped as `<project>_<name>`, matching docker-compose (this is also what `apricot down -v` deletes). When `up` creates a new volume, it is seeded once from the service image — contents, ownership and mode of the directory it mounts over — emulating docker's copy-on-first-use, because Apple Container mounts a bare volume as a root-owned empty directory that non-root users cannot write to ([apple/container#729](https://github.com/apple/container/issues/729)). Seeding needs `/bin/sh` in the image and can be disabled with `APRICOT_DISABLE_VOLUME_INIT=1`. Volumes created by apricot ≤ v1.2.2 under bare names (e.g. `data` instead of `myproject_data`) are no longer used; copy data over or remove them manually if needed.
- **tty / stdin_open**: `container run -t -i` requires stdin to be a real terminal (it fails with `Operation not supported by device` otherwise, even detached). When apricot runs without a terminal on stdin (CI, scripts), `stdin_open` is dropped with a warning so the service still starts.
- **Build context filtering**: `container build` scans every file in the build context — even ones excluded by `.dockerignore` — at a per-file CPU cost ([apple/container#2026](https://github.com/apple/container/issues/2026)), which adds minutes for contexts with large ignored trees (`target/`, `node_modules`, ...). As a workaround, when the context has a `.dockerignore`, apricot builds from a temporary copy containing only the non-ignored files (created with APFS clonefile, so it is fast and consumes no extra disk space, and removed after the build). Set `APRICOT_DISABLE_CONTEXT_FILTER=1` to build from the original context directory.
