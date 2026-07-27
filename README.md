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
apricot up --scale web=3          # start 3 instances of web
apricot up --scale web=3 --scale db=2  # scale multiple services
apricot up -f path/to/docker-compose.yaml  # specify file
apricot up -p myproject                    # specify project name
```

Scaled containers are named `<project>-<service>-<index>` (e.g. `myapp-web-1`, `myapp-web-2`).

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
| `depends_on` | ✅ (startup order + `condition: service_healthy`) |
| `healthcheck` | ✅ (used for `service_healthy` waits) |
| `container_name` | ✅ |
| `restart` | ❌ (not supported) |

## Limitations

- **networks**: Non-default network configuration requires macOS 26 or newer (Apple Container runtime limitation). On older macOS versions, `networks` settings are automatically skipped with a warning.
- **init**: `init: true` is passed through as `container run --init`, which runs an init process that forwards signals and reaps zombie processes (Apple Container v1.1.0+).
- **ulimits**: Both the shorthand (`nofile: 1024`) and long form (`nofile: {soft: 1024, hard: 2048}`) are passed through as `container run --ulimit <type>=<soft>[:<hard>]` (Apple Container v1.1.0+).
- **healthcheck**: Apple Container has no native healthcheck, so apricot runs the `test` command inside the container via `container exec` (honoring `interval` / `timeout` / `retries` / `start_period`). It is used to satisfy `depends_on: { x: { condition: service_healthy } }`, which makes `up` wait for a dependency to become healthy before starting dependents. `condition: service_completed_successfully` is not yet supported.
- **Unsupported keys**: Any service key apricot does not handle (e.g. `deploy`, `restart`, `extends`, `profiles`) is reported with a warning when the compose file is loaded, instead of being silently dropped.
