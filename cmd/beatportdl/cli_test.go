package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCommandLineExitsBeforeSetup(t *testing.T) {
	if arg := os.Getenv("BEATPORTDL_TEST_CLI_ARG"); arg != "" {
		os.Args = []string{"beatportdl", arg}
		main()
		return
	}
	for _, tc := range []struct {
		arg      string
		exitCode int
		output   string
	}{
		{"--help", 0, "downloads-directory"},
		{"--max-download-workers=invalid", 2, "expected an integer"},
	} {
		t.Run(tc.arg, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestCommandLineExitsBeforeSetup$")
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(), "BEATPORTDL_TEST_CLI_ARG="+tc.arg)
			output, err := cmd.CombinedOutput()
			if err != nil {
				if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != tc.exitCode {
					t.Fatalf("unexpected exit: %v; output: %s", err, output)
				}
			} else if tc.exitCode != 0 {
				t.Fatal("expected nonzero exit status")
			}
			if !strings.Contains(string(output), tc.output) || strings.Contains(string(output), "Config file not found") || strings.Contains(string(output), "Logging in") {
				t.Fatalf("CLI did not exit before setup: %s", output)
			}
		})
	}
}
