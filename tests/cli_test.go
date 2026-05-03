package tests

import (
	"os/exec"
	"strings"
	"testing"
)

func TestCLI_InvalidArgs(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		args []string
		want string // substring expected in stderr
	}{
		{"no mode", []string{}, "either -s or -c"},
		{"both modes", []string{"-s", "-c", "-p", "1234"}, "both server and client"},
		{"no port", []string{"-s"}, "missing port"},
		{"invalid port", []string{"-s", "-p", "abc"}, ""},
		{"negative port", []string{"-s", "-p", "-1"}, ""},
		{"port too high", []string{"-s", "-p", "99999"}, ""},
		{"client no address", []string{"-c", "-p", "1234"}, "host"},
		{"negative timeout", []string{"-s", "-p", "1234", "-w", "-1"}, ""},
		{"zero timeout", []string{"-s", "-p", "1234", "-w", "0"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(binaryPath, tt.args...)
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("expected error, got success")
			}
			if tt.want != "" && !strings.Contains(string(out), tt.want) {
				t.Fatalf("expected %q in output, got: %s", tt.want, string(out))
			}
		})
	}
}

func TestCLI_Help(t *testing.T) {
	if err := buildBinary(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binaryPath, "-h")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("expected success: %v", err)
	}
	if !strings.Contains(string(out), "Usage") {
		t.Fatalf("expected usage, got: %s", string(out))
	}
}
