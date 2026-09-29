package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tkzzzzzz6/pvman/internal/ui"
	"github.com/tkzzzzzz6/pvman/internal/update"
)

// version is injected at release time via
// -ldflags "-X main.version=0.7.0". It stays empty for `go install` builds
// (the module version is recovered from the build info at runtime) and for
// plain `go build` (which reports as "dev").
var version = ""

func main() {
	if len(os.Args) > 1 {
		os.Exit(runCLI(os.Args[1], version))
	}

	m := ui.New()
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "pvman: %v\n", err)
		os.Exit(1)
	}
}

// runCLI handles pvman's non-TUI commands and returns the process exit code.
func runCLI(arg, version string) int {
	switch arg {
	case "--update", "update":
		if err := update.Run(version, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "pvman: update failed: %v\n", err)
			return 1
		}
		return 0
	case "--version", "-v", "version":
		fmt.Printf("pvman %s\n", update.EffectiveVersion(version))
		return 0
	case "--help", "-h", "help":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "pvman: unknown argument %q\n\n%s", arg, usage)
		return 2
	}
}

const usage = `pvman - a terminal UI for managing Python virtual environments

usage:
  pvman              launch the TUI
  pvman update       update pvman to the latest release
  pvman --update     same
  pvman version      print the version
  pvman --version    same
  pvman --help       this help
`
