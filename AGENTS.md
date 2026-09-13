# AGENTS.md

Guidance for coding agents working on postmortems.

## Project Overview

A Go web application (`github.com/icco/postmortems`) that indexes and serves technology incident postmortems.

## Commands

```sh
go test ./...    # Run tests
go vet ./...     # Vet code
go run main.go   # Run locally (port 8080 by default)
go build .       # Build binary
```

## Architecture & Conventions

- `main.go` — Entrypoint, HTTP routing, and server setup.
- Data storage and postmortem collection in root package.
- Follow icco Go conventions (`github.com/icco/gutil` for logging and template rendering).
- PR titles and commits must follow Conventional Commits with lowercase subjects.
- Ensure all tests pass before submitting PRs.
