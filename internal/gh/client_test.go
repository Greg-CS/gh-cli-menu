package gh

import (
	"errors"
	"testing"
)

type fakeRunner struct {
	calls []string
	out   string
	err   error
}

func (f *fakeRunner) Run(name string, args ...string) (string, error) {
	f.calls = append(f.calls, name)
	for _, a := range args {
		f.calls = append(f.calls, a)
	}
	return f.out, f.err
}

func TestRun(t *testing.T) {
	fake := &fakeRunner{out: "hello"}
	old := DefaultRunner
	DefaultRunner = fake
	defer func() { DefaultRunner = old }()

	out, err := Run("repo", "view")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "hello" {
		t.Fatalf("expected %q, got %q", "hello", out)
	}

	want := []string{"gh", "repo", "view"}
	if len(fake.calls) != len(want) {
		t.Fatalf("expected %d calls, got %d: %v", len(want), len(fake.calls), fake.calls)
	}
	for i, w := range want {
		if fake.calls[i] != w {
			t.Fatalf("call[%d]: expected %q, got %q", i, w, fake.calls[i])
		}
	}
}

func TestRun_Error(t *testing.T) {
	fake := &fakeRunner{err: errors.New("boom")}
	old := DefaultRunner
	DefaultRunner = fake
	defer func() { DefaultRunner = old }()

	_, err := Run("issue", "list")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestNewCommand(t *testing.T) {
	cmd := NewCommand("pr", "list")
	if cmd.Path == "" {
		t.Fatal("expected command path to be set")
	}
	want := []string{"gh", "pr", "list"}
	if len(cmd.Args) != len(want) {
		t.Fatalf("expected args %v, got %v", want, cmd.Args)
	}
	for i, w := range want {
		if cmd.Args[i] != w {
			t.Fatalf("arg[%d]: expected %q, got %q", i, w, cmd.Args[i])
		}
	}
}
