package cli

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"vlt/internal/favorite"
)

type recordingFZFRunner struct {
	command fzfCommand
	result  fzfResult
	err     error
	calls   int
	run     func(fzfCommand) (fzfResult, error)
}

func (r *recordingFZFRunner) Run(_ context.Context, command fzfCommand) (fzfResult, error) {
	r.calls++
	input, err := io.ReadAll(command.Stdin)
	if err != nil {
		return fzfResult{}, err
	}
	command.Stdin = strings.NewReader(string(input))
	r.command = command
	if r.run != nil {
		return r.run(command)
	}
	return r.result, r.err
}

type fzfExitError int

func (e fzfExitError) Error() string { return "fzf exited" }
func (e fzfExitError) ExitCode() int { return int(e) }

func TestFZFFavoriteSelectorUsesDeterministicSearchableRowsAndOpaqueSelection(t *testing.T) {
	favorites := []favorite.Favorite{
		{Profile: "team-b", Operation: favorite.OperationRead, Path: "secret/z", Note: "reporting"},
		{Profile: "team-a", Operation: favorite.OperationKVGet, Path: "secret/a", Note: "daily\tnote\nsecond line"},
		{Profile: "team-b", Operation: favorite.OperationRead, Path: "secret/a", Note: "$(touch /tmp/not-run); exit 99"},
	}
	want := favorite.NewService(favorites).List()[1]
	runner := &recordingFZFRunner{
		run: func(command fzfCommand) (fzfResult, error) {
			return fzfResult{Stdout: []byte("favorite-000002\tuser controlled output is ignored\n")}, nil
		},
	}
	selector := newFZFFavoriteSelector(
		func(name string) (string, error) {
			if name != "fzf" {
				t.Fatalf("LookPath(%q), want fzf", name)
			}
			return "/test/bin/fzf", nil
		},
		runner,
	)

	selected, err := selector.Select(context.Background(), favorites)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if selected != want {
		t.Fatalf("Select() = %#v, want %#v", selected, want)
	}
	if runner.calls != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls)
	}
	if runner.command.Path != "/test/bin/fzf" {
		t.Fatalf("command path = %q, want resolved fzf path", runner.command.Path)
	}
	wantArguments := []string{
		"--no-exact",
		"--delimiter=\t",
		"--with-nth=2..",
		"--nth=1..",
		"--header=PATH\tNOTE\tPROFILE\tOPERATION",
		"--prompt=Favorite> ",
		"--layout=reverse",
	}
	if !reflect.DeepEqual(runner.command.Arguments, wantArguments) {
		t.Fatalf("fzf arguments = %#v, want %#v", runner.command.Arguments, wantArguments)
	}
	input, err := io.ReadAll(runner.command.Stdin)
	if err != nil {
		t.Fatalf("ReadAll(command input) error = %v", err)
	}
	wantInput := "" +
		"favorite-000001\tsecret/a\tdaily note second line\tteam-a\tkv-get\n" +
		"favorite-000002\tsecret/a\t$(touch /tmp/not-run); exit 99\tteam-b\tread\n" +
		"favorite-000003\tsecret/z\treporting\tteam-b\tread\n"
	if got := string(input); got != wantInput {
		t.Fatalf("fzf input = %q, want %q", got, wantInput)
	}
	for _, argument := range runner.command.Arguments {
		for _, userValue := range []string{"secret/a", "team-a", "$(touch", "exit 99"} {
			if strings.Contains(argument, userValue) {
				t.Fatalf("fzf argument %q contains user value %q", argument, userValue)
			}
		}
	}
}

func TestFZFFavoriteSelectorShowsDashForEmptyNote(t *testing.T) {
	runner := &recordingFZFRunner{result: fzfResult{Stdout: []byte("favorite-000001\n")}}
	selector := newFZFFavoriteSelector(func(string) (string, error) { return "/test/fzf", nil }, runner)

	selected, err := selector.Select(context.Background(), []favorite.Favorite{{
		Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a",
	}})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if selected.Note != "" {
		t.Fatalf("selected note = %q, want unchanged empty note", selected.Note)
	}
	input, err := io.ReadAll(runner.command.Stdin)
	if err != nil {
		t.Fatalf("ReadAll(command input) error = %v", err)
	}
	if got, want := string(input), "favorite-000001\tsecret/a\t-\tteam-a\tread\n"; got != want {
		t.Fatalf("fzf input = %q, want %q", got, want)
	}
}

func TestFZFFavoriteSelectorFailsActionablyWhenFZFIsMissing(t *testing.T) {
	const token = "hvs.synthetic-missing-fzf-token"
	runner := &recordingFZFRunner{}
	selector := newFZFFavoriteSelector(
		func(string) (string, error) { return "", errors.New("lookup failed with VAULT_TOKEN=" + token) },
		runner,
	)

	_, err := selector.Select(context.Background(), []favorite.Favorite{{
		Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a",
	}})
	if err == nil {
		t.Fatal("Select() error = nil, want missing fzf error")
	}
	for _, text := range []string{"fzf", "PATH", "install"} {
		if !strings.Contains(err.Error(), text) {
			t.Errorf("Select() error = %q, want actionable text %q", err, text)
		}
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("Select() error exposed token: %q", err)
	}
	if runner.calls != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls)
	}
}

func TestFZFFavoriteSelectorRedactsProcessFailure(t *testing.T) {
	const token = "hvs.synthetic-fzf-process-token"
	runner := &recordingFZFRunner{
		result: fzfResult{Stderr: []byte("selector failed with VAULT_TOKEN=" + token)},
		err:    errors.New("process failed with token=" + token),
	}
	selector := newFZFFavoriteSelector(func(string) (string, error) { return "/test/fzf", nil }, runner)

	_, err := selector.Select(context.Background(), []favorite.Favorite{{
		Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a",
	}})
	if err == nil {
		t.Fatal("Select() error = nil, want process failure")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("Select() error exposed token: %q", err)
	}
	for _, text := range []string{"run fzf", "selector failed", "[REDACTED]"} {
		if !strings.Contains(err.Error(), text) {
			t.Errorf("Select() error = %q, want %q", err, text)
		}
	}
}

func TestFZFFavoriteSelectorReturnsDistinctCancellation(t *testing.T) {
	tests := []struct {
		name   string
		result fzfResult
		err    error
	}{
		{name: "interrupted process", err: fzfExitError(130)},
		{name: "empty selection", result: fzfResult{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &recordingFZFRunner{result: tt.result, err: tt.err}
			selector := newFZFFavoriteSelector(func(string) (string, error) { return "/test/fzf", nil }, runner)
			selected, err := selector.Select(context.Background(), []favorite.Favorite{{
				Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a",
			}})
			if !errors.Is(err, ErrFavoriteSelectionCanceled) {
				t.Fatalf("Select() error = %v, want ErrFavoriteSelectionCanceled", err)
			}
			if selected != (favorite.Favorite{}) {
				t.Fatalf("Select() = %#v, want no favorite", selected)
			}
		})
	}
}

func TestFZFFavoriteSelectorRejectsUnknownOrMalformedOpaqueSelection(t *testing.T) {
	outputs := []string{
		"favorite-999999\tvisible fields\n",
		"secret/a\tteam-a\tread\n",
		"favorite-000001\tvisible\nfavorite-000001\tsecond\n",
	}
	for _, output := range outputs {
		t.Run(output, func(t *testing.T) {
			runner := &recordingFZFRunner{result: fzfResult{Stdout: []byte(output)}}
			selector := newFZFFavoriteSelector(func(string) (string, error) { return "/test/fzf", nil }, runner)

			selected, err := selector.Select(context.Background(), []favorite.Favorite{{
				Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a",
			}})
			if err == nil || !strings.Contains(err.Error(), "selection") {
				t.Fatalf("Select() error = %v, want selection rejection", err)
			}
			if selected != (favorite.Favorite{}) {
				t.Fatalf("Select() = %#v, want no favorite", selected)
			}
		})
	}
}

func TestFZFFavoriteSelectorRejectsEmptyFavoritesBeforeDiscovery(t *testing.T) {
	lookupCalls := 0
	runner := &recordingFZFRunner{}
	selector := newFZFFavoriteSelector(func(string) (string, error) {
		lookupCalls++
		return "/test/fzf", nil
	}, runner)

	_, err := selector.Select(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "favorite add") {
		t.Fatalf("Select() error = %v, want add guidance", err)
	}
	if lookupCalls != 0 || runner.calls != 0 {
		t.Fatalf("empty selection used process boundaries: lookups = %d, runs = %d", lookupCalls, runner.calls)
	}
}

func TestFZFFavoriteSelectorHonorsCanceledContextBeforeDiscovery(t *testing.T) {
	lookupCalls := 0
	selector := newFZFFavoriteSelector(func(string) (string, error) {
		lookupCalls++
		return "/test/fzf", nil
	}, &recordingFZFRunner{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := selector.Select(ctx, []favorite.Favorite{{
		Profile: "team-a", Operation: favorite.OperationRead, Path: "secret/a",
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Select() error = %v, want context.Canceled", err)
	}
	if lookupCalls != 0 {
		t.Fatalf("LookPath() calls = %d, want 0", lookupCalls)
	}
}
