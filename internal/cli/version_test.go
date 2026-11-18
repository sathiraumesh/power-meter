package cli

import (
	"bytes"
	"strings"
	"testing"
)

// run executes the root command with args and returns its combined output.
func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})
	err := rootCmd.Execute()
	return buf.String(), err
}

func TestVersionCmd(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{name: "default", version: "dev", want: "dev\n"},
		{name: "injected", version: "v1.2.3", want: "v1.2.3\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			old := version
			version = tt.version
			t.Cleanup(func() { version = old })

			got, err := run(t, "version")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestVersionCmdRejectsArgs(t *testing.T) {
	if _, err := run(t, "version", "extra"); err == nil {
		t.Fatal("expected error for extra argument, got nil")
	}
}

func TestRootHelp(t *testing.T) {
	got, err := run(t, "--help")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{"power-meter", "version"} {
		if !strings.Contains(got, want) {
			t.Errorf("help output missing %q:\n%s", want, got)
		}
	}
}
