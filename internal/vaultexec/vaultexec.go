// Package vaultexec provides the process boundary for invoking the official
// Vault CLI.
package vaultexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// StreamMode determines whether Vault uses caller-provided streams or whether
// its output is captured for machine-readable operations.
type StreamMode uint8

const (
	// Delegated leaves standard streams connected to the caller.
	Delegated StreamMode = iota
	// Captured collects standard output and standard error in Result.
	Captured
)

// RedactedValue is substituted for credential-like values in diagnostics.
const RedactedValue = "[REDACTED]"

// EnvironmentOverlay describes variables to replace and variables to remove.
// Keys are matched case-insensitively for consistent, Windows-safe behavior.
type EnvironmentOverlay struct {
	Set   map[string]string
	Unset []string
}

// ProfileEnvironment creates the Vault variables owned by one profile. An
// empty namespace explicitly removes any inherited VAULT_NAMESPACE.
func ProfileEnvironment(address, token, namespace string) EnvironmentOverlay {
	overlay := EnvironmentOverlay{
		Set: map[string]string{
			"VAULT_ADDR":  address,
			"VAULT_TOKEN": token,
		},
	}
	if namespace == "" {
		overlay.Unset = []string{"VAULT_NAMESPACE"}
	} else {
		overlay.Set["VAULT_NAMESPACE"] = namespace
	}
	return overlay
}

// Invocation describes one execution of the Vault CLI. Arguments are passed
// directly as an argument vector and are never interpreted by a shell.
type Invocation struct {
	Arguments       []string
	Environment     EnvironmentOverlay
	Mode            StreamMode
	Stdin           io.Reader
	Stdout          io.Writer
	Stderr          io.Writer
	SensitiveValues []string
}

// Command is the fully resolved process request passed to a Runner.
type Command struct {
	Path        string
	Arguments   []string
	Environment []string
	Mode        StreamMode
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
}

// Result contains output collected in Captured mode. Standard output remains
// raw for machine-readable Vault responses; Executor redacts standard error
// before returning it to callers.
type Result struct {
	Stdout []byte
	Stderr []byte
}

// Runner starts one process without invoking a shell.
type Runner interface {
	Run(context.Context, Command) (Result, error)
}

// Dependencies are the injectable operating-system boundaries used by an
// Executor.
type Dependencies struct {
	LookPath    func(string) (string, error)
	Environment func() []string
	Runner      Runner
}

// Executor discovers and invokes the official Vault CLI.
type Executor struct {
	lookPath    func(string) (string, error)
	environment func() []string
	runner      Runner
}

// NewExecutor constructs an Executor from explicit process dependencies.
func NewExecutor(dependencies Dependencies) *Executor {
	return &Executor{
		lookPath:    dependencies.LookPath,
		environment: dependencies.Environment,
		runner:      dependencies.Runner,
	}
}

// NewOSExecutor constructs an Executor backed by the current process and
// operating system.
func NewOSExecutor() *Executor {
	return NewExecutor(Dependencies{
		LookPath:    exec.LookPath,
		Environment: os.Environ,
		Runner:      OSRunner{},
	})
}

// FindVault resolves Vault through PATH and returns an actionable error when
// it is unavailable.
func (e *Executor) FindVault() (string, error) {
	if e == nil || e.lookPath == nil {
		return "", errors.New("discover Vault CLI: executable lookup is not configured")
	}
	path, err := e.lookPath("vault")
	if err != nil {
		return "", fmt.Errorf("Vault CLI was not found in PATH; install the HashiCorp Vault CLI or add its executable to PATH: %w", err)
	}
	if path == "" {
		return "", errors.New("Vault CLI was not found in PATH; install the HashiCorp Vault CLI or add its executable to PATH")
	}
	return path, nil
}

// Execute discovers Vault before touching the remaining process boundaries,
// overlays its environment, and runs it in the requested stream mode.
func (e *Executor) Execute(ctx context.Context, invocation Invocation) (Result, error) {
	path, err := e.FindVault()
	if err != nil {
		return Result{}, err
	}
	if e.environment == nil {
		return Result{}, errors.New("run Vault CLI: environment lookup is not configured")
	}
	if e.runner == nil {
		return Result{}, errors.New("run Vault CLI: process runner is not configured")
	}

	command := Command{
		Path:        path,
		Arguments:   append([]string(nil), invocation.Arguments...),
		Environment: applyEnvironment(e.environment(), invocation.Environment),
		Mode:        invocation.Mode,
		Stdin:       invocation.Stdin,
		Stdout:      invocation.Stdout,
		Stderr:      invocation.Stderr,
	}
	sensitiveValues := append([]string(nil), invocation.SensitiveValues...)
	for key, value := range invocation.Environment.Set {
		if strings.EqualFold(key, "VAULT_TOKEN") {
			sensitiveValues = append(sensitiveValues, value)
		}
	}
	redact := func(value string) string {
		return RedactDiagnostic(value, sensitiveValues...)
	}

	result, runErr := e.runner.Run(ctx, command)
	if invocation.Mode == Captured && len(result.Stderr) > 0 {
		result.Stderr = []byte(redact(string(result.Stderr)))
	}
	if runErr == nil {
		return result, nil
	}

	diagnostic := strings.TrimSpace(string(result.Stderr))
	safeError := redact(runErr.Error())
	exitCode := ExitCode(runErr)
	if diagnostic == "" {
		return result, &ProcessError{message: "run Vault CLI: " + safeError, exitCode: exitCode}
	}
	return result, &ProcessError{
		message:  fmt.Sprintf("run Vault CLI: %s: %s", safeError, diagnostic),
		exitCode: exitCode,
	}
}

func applyEnvironment(base []string, overlay EnvironmentOverlay) []string {
	replaced := make(map[string]struct{}, len(overlay.Set)+len(overlay.Unset))
	for key := range overlay.Set {
		replaced[strings.ToUpper(key)] = struct{}{}
	}
	for _, key := range overlay.Unset {
		replaced[strings.ToUpper(key)] = struct{}{}
	}

	result := make([]string, 0, len(base)+len(overlay.Set))
	for _, entry := range base {
		key := entry
		if separator := strings.IndexByte(entry, '='); separator >= 0 {
			key = entry[:separator]
		}
		if _, found := replaced[strings.ToUpper(key)]; !found {
			result = append(result, entry)
		}
	}

	keys := make([]string, 0, len(overlay.Set))
	for key := range overlay.Set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result = append(result, key+"="+overlay.Set[key])
	}
	return result
}

// ProcessError exposes only a redacted diagnostic while retaining the
// process exit code needed by the caller.
type ProcessError struct {
	message  string
	exitCode int
}

func (e *ProcessError) Error() string { return e.message }
func (e *ProcessError) ExitCode() int { return e.exitCode }

// ExitCode converts a process failure to the code vlt should return. Normal
// exit codes are unchanged; signal termination uses the conventional 128+N on
// platforms that expose Unix process signals.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitCoder interface{ ExitCode() int }
	if errors.As(err, &exitCoder) {
		if code := exitCoder.ExitCode(); code >= 0 {
			return code
		}
	}
	if code, ok := signalExitCode(err); ok {
		return code
	}
	return 1
}

// OSRunner executes Command directly with os/exec. No shell is involved.
type OSRunner struct{}

// Run implements Runner.
func (OSRunner) Run(ctx context.Context, command Command) (Result, error) {
	cmd := exec.CommandContext(ctx, command.Path, command.Arguments...)
	if command.Environment != nil {
		cmd.Env = make([]string, len(command.Environment))
		copy(cmd.Env, command.Environment)
	}
	cmd.Stdin = command.Stdin

	switch command.Mode {
	case Delegated:
		if cmd.Stdin == nil {
			cmd.Stdin = os.Stdin
		}
		cmd.Stdout = command.Stdout
		if cmd.Stdout == nil {
			cmd.Stdout = os.Stdout
		}
		cmd.Stderr = command.Stderr
		if cmd.Stderr == nil {
			cmd.Stderr = os.Stderr
		}
		return Result{}, cmd.Run()
	case Captured:
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		return Result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, err
	default:
		return Result{}, fmt.Errorf("run Vault CLI: unsupported stream mode %d", command.Mode)
	}
}

var (
	credentialFieldPattern = regexp.MustCompile(`(?i)(\b(?:client_token|vault_token|token)\b["']?\s*(?:->|[=:])\s*["']?)([^\s,"'}]+)`)
	vaultTokenPattern      = regexp.MustCompile(`\b(?:hvs|hvb|hvg|hvr|s)\.[A-Za-z0-9_-]+\b`)
)

// RedactDiagnostic removes caller-known secrets and common Vault token forms
// from text before it is displayed or logged.
func RedactDiagnostic(message string, sensitiveValues ...string) string {
	redacted := message
	for _, value := range sensitiveValues {
		if value != "" {
			redacted = strings.ReplaceAll(redacted, value, RedactedValue)
		}
	}
	redacted = credentialFieldPattern.ReplaceAllString(redacted, `${1}`+RedactedValue)
	redacted = vaultTokenPattern.ReplaceAllString(redacted, RedactedValue)
	return redacted
}
