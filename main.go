package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/gregsvieira/gh-gum/internal/tui"
)

func main() {
	if err := requireGH(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if err := tui.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func requireGH() error {
	if _, err := exec.LookPath("gh"); err != nil {
		return errors.New("gh is not installed or not in PATH. Install it from https://cli.github.com/")
	}
	return nil
}
