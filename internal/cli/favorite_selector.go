package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"unicode"

	"vlt/internal/favorite"
	"vlt/internal/vaultexec"
)

var ErrFavoriteSelectionCanceled = errors.New("favorite selection canceled")

type FavoriteSelector interface {
	Select(context.Context, []favorite.Favorite) (favorite.Favorite, error)
}

type fzfCommand struct {
	Path      string
	Arguments []string
	Stdin     io.Reader
}

type fzfResult struct {
	Stdout []byte
	Stderr []byte
}

type fzfRunner interface {
	Run(context.Context, fzfCommand) (fzfResult, error)
}

type osFZFRunner struct{}

func (osFZFRunner) Run(ctx context.Context, command fzfCommand) (fzfResult, error) {
	process := exec.CommandContext(ctx, command.Path, command.Arguments...)
	process.Stdin = command.Stdin
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	process.Stdout = &stdout
	process.Stderr = &stderr
	err := process.Run()
	return fzfResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}, err
}

type fzfFavoriteSelector struct {
	lookPath func(string) (string, error)
	runner   fzfRunner
}

func NewFZFFavoriteSelector() FavoriteSelector {
	return newFZFFavoriteSelector(exec.LookPath, osFZFRunner{})
}

func newFZFFavoriteSelector(lookPath func(string) (string, error), runner fzfRunner) *fzfFavoriteSelector {
	return &fzfFavoriteSelector{lookPath: lookPath, runner: runner}
}

func (s *fzfFavoriteSelector) Select(ctx context.Context, favorites []favorite.Favorite) (favorite.Favorite, error) {
	if err := ctx.Err(); err != nil {
		return favorite.Favorite{}, err
	}
	ordered := favorite.NewService(favorites).List()
	if len(ordered) == 0 {
		return favorite.Favorite{}, errors.New("select favorite: no favorites configured; add one with 'vlt favorite add'")
	}
	if s == nil || s.lookPath == nil {
		return favorite.Favorite{}, errors.New("select favorite: fzf executable lookup is not configured")
	}
	if s.runner == nil {
		return favorite.Favorite{}, errors.New("select favorite: fzf process runner is not configured")
	}

	path, err := s.lookPath("fzf")
	if err != nil || path == "" {
		diagnostic := ""
		if err != nil {
			diagnostic = ": " + vaultexec.RedactDiagnostic(err.Error())
		}
		return favorite.Favorite{}, errors.New("select favorite: fzf was not found in PATH; install fzf or add its executable to PATH" + diagnostic)
	}

	input, identities := favoriteRows(ordered)
	result, runErr := s.runner.Run(ctx, fzfCommand{
		Path: path,
		Arguments: []string{
			"--no-exact",
			"--delimiter=\t",
			"--with-nth=2..",
			"--nth=1..",
			"--header=PATH\tNOTE\tPROFILE\tOPERATION",
			"--prompt=Favorite> ",
			"--layout=reverse",
		},
		Stdin: strings.NewReader(input),
	})
	if runErr != nil {
		if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
			return favorite.Favorite{}, runErr
		}
		if processExitCode(runErr) == 130 {
			return favorite.Favorite{}, ErrFavoriteSelectionCanceled
		}
		return favorite.Favorite{}, fzfProcessError(runErr, result.Stderr)
	}

	selection := strings.TrimRight(string(result.Stdout), "\r\n")
	if selection == "" {
		return favorite.Favorite{}, ErrFavoriteSelectionCanceled
	}
	if strings.ContainsAny(selection, "\r\n") {
		return favorite.Favorite{}, errors.New("select favorite: fzf returned an invalid selection")
	}
	identifier, _, _ := strings.Cut(selection, "\t")
	selected, found := identities[identifier]
	if !found {
		return favorite.Favorite{}, errors.New("select favorite: fzf returned an unknown selection")
	}
	return selected, nil
}

func favoriteRows(favorites []favorite.Favorite) (string, map[string]favorite.Favorite) {
	var rows strings.Builder
	identities := make(map[string]favorite.Favorite, len(favorites))
	for index, candidate := range favorites {
		identifier := fmt.Sprintf("favorite-%06d", index+1)
		note := candidate.Note
		if note == "" {
			note = "-"
		}
		_, _ = fmt.Fprintf(
			&rows,
			"%s\t%s\t%s\t%s\t%s\n",
			identifier,
			sanitizeFZFField(candidate.Path),
			sanitizeFZFField(note),
			sanitizeFZFField(candidate.Profile),
			sanitizeFZFField(candidate.Operation),
		)
		identities[identifier] = candidate
	}
	return rows.String(), identities
}

func sanitizeFZFField(value string) string {
	return strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, value)
}

func processExitCode(err error) int {
	var exitCoder interface{ ExitCode() int }
	if errors.As(err, &exitCoder) {
		return exitCoder.ExitCode()
	}
	return -1
}

func fzfProcessError(err error, stderr []byte) error {
	safeError := vaultexec.RedactDiagnostic(err.Error())
	diagnostic := strings.TrimSpace(vaultexec.RedactDiagnostic(string(stderr)))
	if diagnostic == "" {
		return fmt.Errorf("select favorite: run fzf: %s", safeError)
	}
	return fmt.Errorf("select favorite: run fzf: %s: %s", safeError, diagnostic)
}
