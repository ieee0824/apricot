# apricot

[![Test](https://github.com/ieee0824/apricot/actions/workflows/test.yml/badge.svg)](https://github.com/ieee0824/apricot/actions/workflows/test.yml)
[![Coverage](https://raw.githubusercontent.com/ieee0824/apricot/badges/coverage.svg)](https://github.com/ieee0824/apricot/actions/workflows/test.yml)

docker compose-like tool for [apple container](https://github.com/apple/container).

[日本語](README.ja.md)

## Goal

Load `docker-compose.yaml` as-is.

## Installation

```bash
go install github.com/ieee0824/apricot/cmd/apricot@latest
```

Build from source:

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
| `ports` | ✅ |
| `volumes` | ✅ |
| `environment` | ✅ |
| `env_file` | ✅ |
| `working_dir` | ✅ |
| `user` | ✅ |
| `entrypoint` | ✅ |
| `command` | ✅ |
| `networks` | ✅ |
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
| `init` | ✅ |
| `ulimits` | ✅ |
| `depends_on` | ✅ (startup order only) |
| `container_name` | ✅ |
| `restart` | ❌ (not supported) |
