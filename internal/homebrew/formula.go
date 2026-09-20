package homebrew

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	stableTagPattern = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	checksumPattern  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

//go:embed vlt.rb.tmpl
var formulaTemplate string

func Render(tag, checksum string) (string, error) {
	if !stableTagPattern.MatchString(tag) {
		return "", fmt.Errorf("render Homebrew formula: tag %q must match vMAJOR.MINOR.PATCH", tag)
	}
	if !checksumPattern.MatchString(checksum) {
		return "", fmt.Errorf("render Homebrew formula: checksum must be 64 lowercase hexadecimal characters")
	}
	if strings.Count(formulaTemplate, "{{TAG}}") != 1 || strings.Count(formulaTemplate, "{{SHA256}}") != 1 {
		return "", fmt.Errorf("render Homebrew formula: template placeholders are invalid")
	}

	formula := strings.Replace(formulaTemplate, "{{TAG}}", tag, 1)
	formula = strings.Replace(formula, "{{SHA256}}", checksum, 1)
	if strings.Contains(formula, "{{") || strings.Contains(formula, "}}") {
		return "", fmt.Errorf("render Homebrew formula: template contains an unresolved placeholder")
	}
	return formula, nil
}

func Write(output, tag, checksum string) error {
	formula, err := Render(tag, checksum)
	if err != nil {
		return err
	}

	directory := filepath.Dir(output)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create Homebrew formula directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".vlt.rb.*")
	if err != nil {
		return fmt.Errorf("create temporary Homebrew formula: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() { _ = os.Remove(temporaryName) }()

	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set Homebrew formula permissions: %w", err)
	}
	if _, err := temporary.WriteString(formula); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write Homebrew formula: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close Homebrew formula: %w", err)
	}
	if err := os.Rename(temporaryName, output); err != nil {
		return fmt.Errorf("publish Homebrew formula: %w", err)
	}
	return nil
}
