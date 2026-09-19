package cli

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"vlt/internal/config"
	"vlt/internal/profile"
)

type fakeProfileRemovalConfirmer struct {
	request ProfileRemovalConfirmation
	result  bool
	err     error
	calls   int
}

type fakeProfileCascade struct {
	linkedCount int
	countErr    error
	removeErr   error
	countNames  []string
	removals    []profileCascadeRemoval
}

type profileCascadeRemoval struct {
	name     string
	approved bool
}

func (f *fakeProfileCascade) LinkedCount(_ context.Context, name string) (int, error) {
	f.countNames = append(f.countNames, name)
	return f.linkedCount, f.countErr
}

func (f *fakeProfileCascade) Remove(_ context.Context, name string, approved bool) error {
	f.removals = append(f.removals, profileCascadeRemoval{name: name, approved: approved})
	return f.removeErr
}

func (f *fakeProfileRemovalConfirmer) Confirm(_ context.Context, request ProfileRemovalConfirmation) (bool, error) {
	f.calls++
	f.request = request
	return f.result, f.err
}

func TestProfileHandlerRemoveSelectsAndConfirms(t *testing.T) {
	tests := []struct {
		name               string
		active             string
		wantLeavesNoActive bool
	}{
		{name: "active profile", active: "team-a", wantLeavesNoActive: true},
		{name: "inactive profile", active: "team-b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			teamA := managementTestProfile("team-a")
			teamB := managementTestProfile("team-b")
			store := &fakeProfileStore{configuration: config.Configuration{
				Profiles: []profile.Profile{teamB, teamA}, ActiveProfile: tt.active,
			}}
			selector := &fakeProfileSelector{selected: "team-a"}
			confirmer := &fakeProfileRemovalConfirmer{result: true}
			mutator := &fakeProfileMutator{}
			var output bytes.Buffer
			handler := NewProfileHandler(ProfileDependencies{
				Profiles: store, Mutations: mutator, Output: &output,
				Terminal: switchTerminal{prompts: true}, Selector: selector, RemovalConfirmer: confirmer,
			})

			if err := handler(context.Background(), []string{"remove"}); err != nil {
				t.Fatalf("profile remove error = %v", err)
			}
			if store.loads != 1 {
				t.Errorf("profile loads = %d, want one snapshot", store.loads)
			}
			if !reflect.DeepEqual(selector.names, []string{"team-a", "team-b"}) || selector.active != tt.active {
				t.Errorf("selector request: names=%#v active=%q", selector.names, selector.active)
			}
			wantRequest := ProfileRemovalConfirmation{Name: "team-a", LeavesNoActiveProfile: tt.wantLeavesNoActive}
			if confirmer.request != wantRequest {
				t.Errorf("confirmation request = %#v, want %#v", confirmer.request, wantRequest)
			}
			if !reflect.DeepEqual(mutator.removed, []string{"team-a"}) {
				t.Errorf("removed profiles = %#v, want team-a", mutator.removed)
			}
			if got, want := output.String(), "Removed profile \"team-a\".\n"; got != want {
				t.Errorf("output = %q, want %q", got, want)
			}
		})
	}
}

func TestProfileHandlerRemoveStopsBeforeMutationWhenNotConfirmed(t *testing.T) {
	tests := []struct {
		name   string
		result bool
		err    error
	}{
		{name: "decline"},
		{name: "cancel", err: errors.New("user aborted")},
		{name: "interrupt", err: context.Canceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			current := managementTestProfile("team-a")
			configuration := config.Configuration{Profiles: []profile.Profile{current}, ActiveProfile: "team-a"}
			store := &fakeProfileStore{configuration: configuration}
			mutator := &fakeProfileMutator{}
			confirmer := &fakeProfileRemovalConfirmer{result: tt.result, err: tt.err}
			var output bytes.Buffer
			handler := NewProfileHandler(ProfileDependencies{
				Profiles: store, Mutations: mutator, Output: &output,
				Terminal: switchTerminal{prompts: true}, Selector: &fakeProfileSelector{selected: "team-a"},
				RemovalConfirmer: confirmer,
			})

			err := handler(context.Background(), []string{"remove"})
			if tt.err == nil && err != nil {
				t.Fatalf("profile remove error = %v, want declined success", err)
			}
			if tt.err != nil {
				if err == nil || !errors.Is(err, tt.err) && !strings.Contains(err.Error(), tt.err.Error()) {
					t.Fatalf("profile remove error = %v, want %v", err, tt.err)
				}
			}
			if len(mutator.removed) != 0 || !reflect.DeepEqual(store.configuration, configuration) {
				t.Errorf("unconfirmed removal changed state: removed=%#v configuration=%#v", mutator.removed, store.configuration)
			}
			if output.Len() != 0 {
				t.Errorf("output = %q, want empty", output.String())
			}
		})
	}
}

func TestProfileHandlerRemoveWithNameBypassesInteractiveServices(t *testing.T) {
	store := &fakeProfileStore{}
	selector := &fakeProfileSelector{}
	confirmer := &fakeProfileRemovalConfirmer{}
	mutator := &fakeProfileMutator{}
	handler := NewProfileHandler(ProfileDependencies{
		Profiles: store, Mutations: mutator, Output: &bytes.Buffer{},
		Terminal: switchTerminal{prompts: true}, Selector: selector, RemovalConfirmer: confirmer,
	})

	if err := handler(context.Background(), []string{"remove", "team-a"}); err != nil {
		t.Fatalf("profile remove error = %v", err)
	}
	if store.loads != 0 || selector.calls != 0 || confirmer.calls != 0 {
		t.Errorf("explicit removal used interactive services: loads=%d selector=%d confirmer=%d", store.loads, selector.calls, confirmer.calls)
	}
	if !reflect.DeepEqual(mutator.removed, []string{"team-a"}) {
		t.Errorf("removed profiles = %#v, want team-a", mutator.removed)
	}
}

func TestProfileHandlerRemoveWithoutNameReturnsAutomaticHelpOutsideTerminal(t *testing.T) {
	store := &fakeProfileStore{}
	confirmer := &fakeProfileRemovalConfirmer{}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{
		Profiles: store, Output: &output, Terminal: switchTerminal{},
		Selector: &fakeProfileSelector{}, RemovalConfirmer: confirmer,
	})

	err := handler(context.Background(), []string{"remove"})
	requireAutomaticHelp(t, err, profileRemoveHelpText)
	if store.loads != 0 || confirmer.calls != 0 {
		t.Errorf("non-terminal removal used services: loads=%d confirmer=%d", store.loads, confirmer.calls)
	}
	if output.Len() != 0 {
		t.Errorf("output = %q, want empty", output.String())
	}
}

func TestProfileHandlerRemoveRedactsInteractiveFailures(t *testing.T) {
	const token = "hvs.synthetic-remove-confirm-token"
	store := &fakeProfileStore{configuration: config.Configuration{
		Profiles: []profile.Profile{managementTestProfile("team-a")}, ActiveProfile: "team-a",
	}}
	mutator := &fakeProfileMutator{}
	confirmer := &fakeProfileRemovalConfirmer{err: errors.New("confirm failed with VAULT_TOKEN=" + token)}
	handler := NewProfileHandler(ProfileDependencies{
		Profiles: store, Mutations: mutator, Output: &bytes.Buffer{},
		Terminal: switchTerminal{prompts: true}, Selector: &fakeProfileSelector{selected: "team-a"},
		RemovalConfirmer: confirmer,
	})

	err := handler(context.Background(), []string{"remove"})
	if err == nil {
		t.Fatal("profile remove error = nil, want confirmation failure")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("profile remove error exposed token: %q", err)
	}
	if len(mutator.removed) != 0 {
		t.Errorf("confirmation failure removed profiles: %#v", mutator.removed)
	}
}

func TestProfileRemoveWithLinkedFavoritesRequiresNonTerminalFlag(t *testing.T) {
	cascade := &fakeProfileCascade{linkedCount: 2}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{
		Cascade: cascade, Output: &output, Terminal: switchTerminal{},
	})

	err := handler(context.Background(), []string{"remove", "team-a"})
	if err == nil {
		t.Fatal("profile remove error = nil, want linked-favorite refusal")
	}
	for _, text := range []string{"2 linked favorites", "--remove-favorites", "Usage: " + profileRemoveUsage} {
		if !strings.Contains(err.Error(), text) {
			t.Errorf("profile remove error = %q, want %q", err, text)
		}
	}
	if len(cascade.removals) != 0 || output.Len() != 0 {
		t.Fatalf("refused removal changed state: removals=%#v output=%q", cascade.removals, output.String())
	}
}

func TestProfileRemoveWithLinkedFavoritesPromptsWithExactCount(t *testing.T) {
	tests := []struct {
		name      string
		confirmed bool
		wantCalls int
	}{
		{name: "decline"},
		{name: "confirm", confirmed: true, wantCalls: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cascade := &fakeProfileCascade{linkedCount: 3}
			confirmer := &fakeProfileRemovalConfirmer{result: tt.confirmed}
			store := &fakeProfileStore{configuration: config.Configuration{ActiveProfile: "team-a"}}
			var output bytes.Buffer
			handler := NewProfileHandler(ProfileDependencies{
				Profiles: store, Cascade: cascade, Output: &output,
				Terminal: switchTerminal{prompts: true}, RemovalConfirmer: confirmer,
			})

			if err := handler(context.Background(), []string{"remove", "team-a"}); err != nil {
				t.Fatalf("profile remove error = %v", err)
			}
			wantRequest := ProfileRemovalConfirmation{
				Name: "team-a", LeavesNoActiveProfile: true, LinkedFavorites: 3,
			}
			if confirmer.request != wantRequest {
				t.Fatalf("confirmation request = %#v, want %#v", confirmer.request, wantRequest)
			}
			if len(cascade.removals) != tt.wantCalls {
				t.Fatalf("cascade removals = %#v, want %d", cascade.removals, tt.wantCalls)
			}
			if tt.confirmed && !reflect.DeepEqual(cascade.removals, []profileCascadeRemoval{{name: "team-a", approved: true}}) {
				t.Fatalf("cascade removals = %#v, want approved team-a", cascade.removals)
			}
			if !tt.confirmed && output.Len() != 0 {
				t.Fatalf("declined output = %q, want empty", output.String())
			}
		})
	}
}

func TestProfileRemoveFavoritesFlagSkipsPrompt(t *testing.T) {
	cascade := &fakeProfileCascade{linkedCount: 2}
	confirmer := &fakeProfileRemovalConfirmer{err: errors.New("must not prompt")}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{
		Cascade: cascade, Output: &output, Terminal: switchTerminal{}, RemovalConfirmer: confirmer,
	})

	if err := handler(context.Background(), []string{"remove", "team-a", "--remove-favorites"}); err != nil {
		t.Fatalf("profile remove error = %v", err)
	}
	if confirmer.calls != 0 {
		t.Fatalf("confirmation calls = %d, want 0", confirmer.calls)
	}
	want := []profileCascadeRemoval{{name: "team-a", approved: true}}
	if !reflect.DeepEqual(cascade.removals, want) {
		t.Fatalf("cascade removals = %#v, want %#v", cascade.removals, want)
	}
}

func TestProfileRemoveWithoutLinkedFavoritesStaysImmediate(t *testing.T) {
	cascade := &fakeProfileCascade{}
	confirmer := &fakeProfileRemovalConfirmer{err: errors.New("must not prompt")}
	handler := NewProfileHandler(ProfileDependencies{
		Cascade: cascade, Output: &bytes.Buffer{}, Terminal: switchTerminal{prompts: true}, RemovalConfirmer: confirmer,
	})

	if err := handler(context.Background(), []string{"remove", "team-a"}); err != nil {
		t.Fatalf("profile remove error = %v", err)
	}
	if confirmer.calls != 0 {
		t.Fatalf("confirmation calls = %d, want 0", confirmer.calls)
	}
	want := []profileCascadeRemoval{{name: "team-a", approved: false}}
	if !reflect.DeepEqual(cascade.removals, want) {
		t.Fatalf("cascade removals = %#v, want %#v", cascade.removals, want)
	}
}
