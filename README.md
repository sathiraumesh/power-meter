<p align="center">
  <img src="assets/logo.svg" alt="power-meter logo" width="200">
</p>

<h1 align="center">power-meter</h1>

<p align="center">A command-line tool written in Go, built with <a href="https://github.com/spf13/cobra">Cobra</a>.</p>

## Requirements

- Go 1.26.2 or later
- `make` (optional, for the Makefile targets)

## Getting started

```bash
git clone <repo-url>
cd power-meter
make build
./bin/power-meter --help
```

Without `make`:

```bash
go build -o bin/power-meter ./cmd/power-meter
```

## Usage

```bash
power-meter [command]
```

| Command      | Description                                        |
|--------------|----------------------------------------------------|
| `version`    | Print the version                                  |
| `completion` | Generate a shell completion script (bash, zsh, ...) |
| `help`       | Help about any command                             |

Run `power-meter <command> --help` for details on a command.

## Development

| Target                   | Description                                      |
|--------------------------|--------------------------------------------------|
| `make build`             | Build `bin/power-meter`                          |
| `make run ARGS="..."`    | Build and run with the given arguments           |
| `make test`              | Run all tests                                    |
| `make fmt`               | Format code                                      |
| `make vet`               | Run `go vet`                                     |
| `make tidy`              | Tidy `go.mod` / `go.sum`                         |
| `make clean`             | Remove build output                              |

### Versioning

The version printed by `power-meter version` is set at build time from
`git describe --tags`, falling back to `dev` when there are no tags.
To set it explicitly:

```bash
make build VERSION=v1.0.0
```

### Project layout

```
.
├── cmd/power-meter/   # main package: entry point only
└── internal/cli/      # Cobra commands (root, version, ...)
```

### Adding a command

Create a file in `internal/cli/` and register the command in `init()`:

```go
func init() {
	rootCmd.AddCommand(myCmd())
}
```

Print via `cmd.OutOrStdout()` rather than `fmt.Println` so output can be
captured in tests.
