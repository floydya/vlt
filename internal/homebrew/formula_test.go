package homebrew

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validChecksum = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestRenderFormula(t *testing.T) {
	formula, err := Render("v1.2.3", validChecksum)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}

	wants := []string{
		`url "https://github.com/floydya/vlt/archive/refs/tags/v1.2.3.tar.gz"`,
		`sha256 "` + validChecksum + `"`,
		`license "MIT"`,
		`depends_on :macos`,
		`depends_on "go" => :build`,
		`system "go", "mod", "download"`,
		`system "go", "build", *std_go_args(output: bin/"vlt"), "./cmd/vlt"`,
		`generate_completions_from_executable(bin/"vlt", "completion")`,
		`assert_match "_vlt_completion", shell_output("#{bin}/vlt completion bash")`,
	}
	for _, want := range wants {
		if !strings.Contains(formula, want) {
			t.Errorf("Render() output does not contain %q", want)
		}
	}
	if strings.Contains(formula, "{{") || strings.Contains(formula, "}}") {
		t.Errorf("Render() output contains an unresolved placeholder")
	}
}

func TestRenderFormulaRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		checksum string
	}{
		{name: "missing tag prefix", tag: "1.2.3", checksum: validChecksum},
		{name: "leading zero", tag: "v01.2.3", checksum: validChecksum},
		{name: "missing patch", tag: "v1.2", checksum: validChecksum},
		{name: "prerelease", tag: "v1.2.3-rc.1", checksum: validChecksum},
		{name: "short checksum", tag: "v1.2.3", checksum: "abc123"},
		{name: "uppercase checksum", tag: "v1.2.3", checksum: strings.ToUpper(validChecksum)},
		{name: "non-hex checksum", tag: "v1.2.3", checksum: strings.Repeat("g", 64)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Render(tt.tag, tt.checksum); err == nil {
				t.Fatalf("Render(%q, %q) error = nil", tt.tag, tt.checksum)
			}
		})
	}
}

func TestWriteFormulaPreservesExistingFileOnInvalidInput(t *testing.T) {
	output := filepath.Join(t.TempDir(), "Formula", "vlt.rb")
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(output, []byte("keep\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	if err := Write(output, "invalid", validChecksum); err == nil {
		t.Fatal("Write() error = nil")
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != "keep\n" {
		t.Errorf("output = %q, want unchanged", got)
	}
}

func TestWriteFormulaCreatesParentDirectory(t *testing.T) {
	output := filepath.Join(t.TempDir(), "nested", "Formula", "vlt.rb")

	if err := Write(output, "v1.2.3", validChecksum); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	got, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !strings.Contains(string(got), "class Vlt < Formula") {
		t.Errorf("output does not contain formula class")
	}
}
