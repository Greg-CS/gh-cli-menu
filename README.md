# gh-gum

A GitHub CLI (`gh`) extension built with Go and [Bubble Tea](https://github.com/charmbracelet/bubbletea) that provides an interactive TUI menu for common `gh` commands.

## Description

`gh-gum` simplifies your GitHub CLI workflow by replacing repetitive command typing with a keyboard-driven TUI. Instead of memorizing lengthy `gh` commands, filter and select actions from a live-searchable menu. Interactive commands (e.g. `gh pr create`) suspend the TUI, run normally, and resume when they exit.

## Installation

### Prerequisites

- [Go 1.22+](https://go.dev/dl/)
- [GitHub CLI (`gh`)](https://cli.github.com/)

### Build from source

```bash
git clone <your-repo>/gh-gum
cd gh-gum
go build -o gh-gum .
```

### Install as a gh extension

```bash
gh extension install <your-repo>/gh-gum
```

## Usage

```bash
Method 1: Local binary execution (development)
bash
go build -o gh-gum.exe .
.\gh-gum.exe
Method 2: Install as local gh extension (tests the real gh gum flow)
gh extensions are just binaries named gh-<extension> placed in a specific directory.

bash
# Build the binary with the exact name gh expects
go build -o gh-gum.exe .
 
# Create the local extension directory
mkdir "%LOCALAPPDATA%\GitHub CLI\extensions\gh-gum"
 
# Copy the binary there
copy gh-gum.exe "%LOCALAPPDATA%\GitHub CLI\extensions\gh-gum\"
 
# Now gh recognizes it
# gh gum
# or
gh gum
```

- **↑ / k** – move up
- **↓ / j** – move down
- **/** – filter the menu
- **Enter** – execute the selected command
- **q / ctrl+c** – quit

## Architecture

The project follows a lightweight layered structure inspired by clean architecture principles without DDD ceremony:

```
gh-cli-menu/
├── main.go                      # Entry point
├── internal/
│   ├── tui/
│   │   └── app.go               # Bubble Tea model, Update/View/Init
│   ├── commands/
│   │   └── commands.go          # Command definitions grouped by Kind (Issues, PRs, etc.)
│   ├── gh/
│   │   └── client.go            # gh CLI wrapper + testable Runner interface
│   └── ui/
│       └── styles.go            # Lipgloss theme constants
├── go.mod
└── README.md
```

- **`internal/tui/`** owns all UI state and rendering.
- **`internal/commands/`** owns what commands exist and how they map to `gh`.
- **`internal/gh/`** abstracts the `gh` binary so tests can inject fakes.
- **`internal/ui/`** centralizes all colors and styles.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.
