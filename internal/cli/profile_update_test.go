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

func TestProfileHandlerUpdateUsesPopulatedReadOnlyNameForm(t *testing.T) {
	current := profile.Profile{
		Name: "team-a", Address: "https://vault.example.com", Username: "alice",
		AuthPath: "oidc", Namespace: "engineering",
	}
	completed := current
	completed.Name = "renamed"
	completed.Address = "https://new.example.com"
	completed.Namespace = "platform"
	completed.AllowInsecure = true
	completed.Color = "#A1B2C3"
	store := &fakeProfileStore{configuration: config.Configuration{Profiles: []profile.Profile{current}}}
	mutator := &fakeProfileMutator{}
	form := &fakeProfileForm{result: completed}
	selector := &fakeProfileSelector{selected: "team-b"}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{
		Profiles: store, Mutations: mutator, Output: &output,
		Terminal: switchTerminal{prompts: true}, Selector: selector, Form: form,
	})

	if err := handler(context.Background(), []string{"update", "team-a"}); err != nil {
		t.Fatalf("profile update error = %v", err)
	}
	if store.loads != 1 {
		t.Errorf("profile loads = %d, want one snapshot", store.loads)
	}
	if form.request.Profile != current || form.request.NameEditable {
		t.Errorf("form request = %#v, want populated profile with read-only name", form.request)
	}
	if selector.calls != 0 {
		t.Errorf("selector calls = %d, want 0 for named update", selector.calls)
	}
	if len(mutator.updated) != 1 {
		t.Fatalf("updates = %#v, want one", mutator.updated)
	}
	got := mutator.updated[0]
	if got.name != "team-a" {
		t.Errorf("updated name = %q, want stable team-a", got.name)
	}
	if got.changes.Address == nil || *got.changes.Address != completed.Address {
		t.Errorf("address change = %#v, want %q", got.changes.Address, completed.Address)
	}
	if got.changes.Namespace == nil || *got.changes.Namespace != completed.Namespace {
		t.Errorf("namespace change = %#v, want %q", got.changes.Namespace, completed.Namespace)
	}
	if got.changes.AllowInsecure == nil || *got.changes.AllowInsecure != completed.AllowInsecure {
		t.Errorf("allow insecure change = %#v, want %t", got.changes.AllowInsecure, completed.AllowInsecure)
	}
	if got.changes.Color == nil || *got.changes.Color != completed.Color {
		t.Errorf("color change = %#v, want %q", got.changes.Color, completed.Color)
	}
	if got.changes.Username != nil || got.changes.AuthPath != nil {
		t.Errorf("unchanged fields = %#v, want nil", got.changes)
	}
	if got, want := output.String(), "Updated profile \"team-a\".\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestProfileHandlerUpdateFormCanClearColor(t *testing.T) {
	current := managementTestProfile("team-a")
	current.Color = "#A1B2C3"
	completed := current
	completed.Color = ""
	mutator := &fakeProfileMutator{}
	handler := NewProfileHandler(ProfileDependencies{
		Profiles:  &fakeProfileStore{configuration: config.Configuration{Profiles: []profile.Profile{current}}},
		Mutations: mutator, Output: &bytes.Buffer{}, Terminal: switchTerminal{prompts: true},
		Form: &fakeProfileForm{result: completed},
	})
	if err := handler(context.Background(), []string{"update", "team-a"}); err != nil {
		t.Fatalf("profile update error = %v", err)
	}
	if len(mutator.updated) != 1 || mutator.updated[0].changes.Color == nil || *mutator.updated[0].changes.Color != "" {
		t.Fatalf("form color update = %#v, want explicit clear", mutator.updated)
	}
}

func TestProfileHandlerUpdateSelectsProfileBeforeForm(t *testing.T) {
	teamA := managementTestProfile("team-a")
	teamB := managementTestProfile("team-b")
	store := &fakeProfileStore{configuration: config.Configuration{
		Profiles: []profile.Profile{teamB, teamA}, ActiveProfile: "team-b",
	}}
	mutator := &fakeProfileMutator{}
	form := &fakeProfileForm{result: teamA}
	selector := &fakeProfileSelector{selected: "team-a"}
	handler := NewProfileHandler(ProfileDependencies{
		Profiles: store, Mutations: mutator, Output: &bytes.Buffer{},
		Terminal: switchTerminal{prompts: true}, Selector: selector, Form: form,
	})

	if err := handler(context.Background(), []string{"update"}); err != nil {
		t.Fatalf("profile update error = %v", err)
	}
	if !reflect.DeepEqual(selector.names, []string{"team-a", "team-b"}) || selector.active != "team-b" {
		t.Errorf("selector request: names=%#v active=%q", selector.names, selector.active)
	}
	if store.loads != 1 {
		t.Errorf("profile loads = %d, want one snapshot", store.loads)
	}
	if form.request.Profile != teamA || form.request.NameEditable {
		t.Errorf("form request = %#v, want selected team-a with read-only name", form.request)
	}
	if len(mutator.updated) != 1 || mutator.updated[0].name != "team-a" {
		t.Errorf("updates = %#v, want selected team-a", mutator.updated)
	}
}

func TestProfileHandlerUpdateFlagsBypassSelectionAndForm(t *testing.T) {
	store := &fakeProfileStore{}
	mutator := &fakeProfileMutator{}
	form := &fakeProfileForm{}
	selector := &fakeProfileSelector{}
	handler := NewProfileHandler(ProfileDependencies{
		Profiles: store, Mutations: mutator, Output: &bytes.Buffer{},
		Terminal: switchTerminal{prompts: true}, Selector: selector, Form: form,
	})

	arguments := []string{"update", "team-a", "--address", "https://new.example.com", "--namespace="}
	if err := handler(context.Background(), arguments); err != nil {
		t.Fatalf("profile update error = %v", err)
	}
	if store.loads != 0 || selector.calls != 0 || form.calls != 0 {
		t.Errorf("direct update used interactive services: loads=%d selector=%d form=%d", store.loads, selector.calls, form.calls)
	}
	if len(mutator.updated) != 1 {
		t.Fatalf("updates = %#v, want one", mutator.updated)
	}
	changes := mutator.updated[0].changes
	if changes.Address == nil || *changes.Address != "https://new.example.com" {
		t.Errorf("address change = %#v", changes.Address)
	}
	if changes.Namespace == nil || *changes.Namespace != "" {
		t.Errorf("namespace change = %#v, want explicit empty", changes.Namespace)
	}
	if changes.Username != nil || changes.AuthPath != nil {
		t.Errorf("omitted changes = %#v, want nil", changes)
	}
}

func TestProfileHandlerUpdateIncompleteReturnsAutomaticHelp(t *testing.T) {
	store := &fakeProfileStore{configuration: config.Configuration{Profiles: []profile.Profile{managementTestProfile("team-a")}}}
	mutator := &fakeProfileMutator{}
	form := &fakeProfileForm{}
	selector := &fakeProfileSelector{}
	for _, tt := range []struct {
		name      string
		arguments []string
		terminal  switchTerminal
	}{
		{name: "missing selection outside terminal", arguments: []string{"update"}},
		{name: "missing changes outside terminal", arguments: []string{"update", "team-a"}},
		{name: "flags without name", arguments: []string{"update", "--address", "https://new.example.com"}, terminal: switchTerminal{prompts: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			handler := NewProfileHandler(ProfileDependencies{
				Profiles: store, Mutations: mutator, Output: &output, Terminal: tt.terminal, Selector: selector, Form: form,
			})

			err := handler(context.Background(), tt.arguments)

			requireAutomaticHelp(t, err, profileUpdateHelpText)
			if output.Len() != 0 {
				t.Errorf("output = %q, want empty", output.String())
			}
		})
	}
	if store.loads != 0 || selector.calls != 0 || form.calls != 0 || len(mutator.updated) != 0 {
		t.Errorf("incomplete update used services: loads=%d selector=%d form=%d updates=%#v", store.loads, selector.calls, form.calls, mutator.updated)
	}
}

func TestProfileHandlerUpdateFormFailureDoesNotMutate(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
	}{
		{name: "invalid", err: errors.New("invalid form")},
		{name: "cancel", err: errors.New("user aborted with hvs.synthetic-update-form-token")},
		{name: "interrupt", err: context.Canceled},
	} {
		t.Run(tt.name, func(t *testing.T) {
			current := managementTestProfile("team-a")
			store := &fakeProfileStore{configuration: config.Configuration{Profiles: []profile.Profile{current}}}
			mutator := &fakeProfileMutator{}
			form := &fakeProfileForm{err: tt.err}
			var output bytes.Buffer
			handler := NewProfileHandler(ProfileDependencies{
				Profiles: store, Mutations: mutator, Output: &output,
				Terminal: switchTerminal{prompts: true}, Form: form,
			})

			err := handler(context.Background(), []string{"update", "team-a"})
			if err == nil {
				t.Fatal("profile update error = nil, want form failure")
			}
			if strings.Contains(err.Error(), "synthetic-update-form-token") {
				t.Fatalf("profile update error exposed token: %q", err)
			}
			if len(mutator.updated) != 0 || store.configuration.Profiles[0] != current {
				t.Errorf("form failure changed state: updates=%#v profile=%#v", mutator.updated, store.configuration.Profiles[0])
			}
			if output.Len() != 0 {
				t.Errorf("output = %q, want empty", output.String())
			}
		})
	}
}

func TestProfileHandlerUpdateRedactsMutationFailureAfterForm(t *testing.T) {
	const token = "hvs.synthetic-update-mutation-token"
	current := managementTestProfile("team-a")
	updated := current
	updated.Address = "https://new.example.com"
	store := &fakeProfileStore{configuration: config.Configuration{Profiles: []profile.Profile{current}}}
	mutator := &fakeProfileMutator{updateErr: errors.New("authentication failed with VAULT_TOKEN=" + token)}
	form := &fakeProfileForm{result: updated}
	var output bytes.Buffer
	handler := NewProfileHandler(ProfileDependencies{
		Profiles: store, Mutations: mutator, Output: &output,
		Terminal: switchTerminal{prompts: true}, Form: form,
	})

	err := handler(context.Background(), []string{"update", "team-a"})
	if err == nil {
		t.Fatal("profile update error = nil, want mutation failure")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("profile update error exposed token: %q", err)
	}
	if output.Len() != 0 {
		t.Errorf("output = %q, want empty", output.String())
	}
}
