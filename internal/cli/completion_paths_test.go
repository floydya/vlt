package cli

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"vlt/internal/config"
	"vlt/internal/profile"
)

type completionCredentialGetter struct {
	token string
	err   error
	names []string
}

func (g *completionCredentialGetter) Get(_ context.Context, name string) (string, error) {
	g.names = append(g.names, name)
	return g.token, g.err
}

func TestResolvePathCompletionContextSelectsActiveOrExplicitProfile(t *testing.T) {
	profiles := []profile.Profile{
		{Name: "team-a", Address: "https://a.example.invalid", Username: "a", AuthPath: "oidc", Namespace: "namespace-a"},
		{Name: "team-b", Address: "https://b.example.invalid", Username: "b", AuthPath: "oidc", Namespace: "namespace-b"},
	}
	for _, tt := range []struct {
		name     string
		explicit string
		selected profile.Profile
	}{
		{name: "active", selected: profiles[0]},
		{name: "explicit override", explicit: "team-b", selected: profiles[1]},
	} {
		t.Run(tt.name, func(t *testing.T) {
			loader := &completionProfileLoader{configuration: config.Configuration{Profiles: profiles, ActiveProfile: "team-a"}}
			credentials := &completionCredentialGetter{token: "synthetic-token"}
			got, ok := resolvePathCompletionContext(context.Background(), tt.explicit, CompletionDependencies{
				Profiles: loader, Credentials: credentials,
			})
			if !ok {
				t.Fatal("completion context unavailable, want selected profile")
			}
			if got.profile != tt.selected || got.token != "synthetic-token" {
				t.Errorf("completion context has wrong profile metadata or token")
			}
			if !reflect.DeepEqual(credentials.names, []string{tt.selected.Name}) {
				t.Errorf("credential reads = %q, want selected profile only", credentials.names)
			}
			if loader.loads != 1 {
				t.Errorf("configuration loads = %d, want one", loader.loads)
			}
		})
	}
}

func TestResolvePathCompletionContextFailsQuietlyBeforeUnsafeAccess(t *testing.T) {
	valid := profile.Profile{Name: "team-a", Address: "https://a.example.invalid", Username: "a", AuthPath: "oidc"}
	for _, tt := range []struct {
		name          string
		configuration config.Configuration
		loadErr       error
		explicit      string
		token         string
		keyringErr    error
		wantReads     int
	}{
		{name: "no active profile", configuration: config.Configuration{Profiles: []profile.Profile{valid}}},
		{name: "missing explicit profile", configuration: config.Configuration{Profiles: []profile.Profile{valid}, ActiveProfile: valid.Name}, explicit: "missing"},
		{name: "invalid explicit name", configuration: config.Configuration{Profiles: []profile.Profile{valid}, ActiveProfile: valid.Name}, explicit: "bad name"},
		{name: "configuration failure", loadErr: errors.New("private configuration failure")},
		{name: "invalid profile", configuration: config.Configuration{Profiles: []profile.Profile{{Name: valid.Name, Address: "not-a-url", Username: "a", AuthPath: "oidc"}}, ActiveProfile: valid.Name}},
		{name: "HTTP without opt-in", configuration: config.Configuration{Profiles: []profile.Profile{{Name: valid.Name, Address: "http://local.example.invalid", Username: "a", AuthPath: "oidc"}}, ActiveProfile: valid.Name}},
		{name: "missing token", configuration: config.Configuration{Profiles: []profile.Profile{valid}, ActiveProfile: valid.Name}, wantReads: 1},
		{name: "blank token", configuration: config.Configuration{Profiles: []profile.Profile{valid}, ActiveProfile: valid.Name}, token: " \t", wantReads: 1},
		{name: "control in token", configuration: config.Configuration{Profiles: []profile.Profile{valid}, ActiveProfile: valid.Name}, token: "bad\nvalue", wantReads: 1},
		{name: "keyring failure", configuration: config.Configuration{Profiles: []profile.Profile{valid}, ActiveProfile: valid.Name}, keyringErr: errors.New("private keyring failure"), wantReads: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			loader := &completionProfileLoader{configuration: tt.configuration, err: tt.loadErr}
			credentials := &completionCredentialGetter{token: tt.token, err: tt.keyringErr}
			got, ok := resolvePathCompletionContext(context.Background(), tt.explicit, CompletionDependencies{
				Profiles: loader, Credentials: credentials,
			})
			if ok || got != (pathCompletionContext{}) {
				t.Fatal("completion context exposed a profile after an invalid selection or token failure")
			}
			if len(credentials.names) != tt.wantReads {
				t.Errorf("credential reads = %d, want %d", len(credentials.names), tt.wantReads)
			}
		})
	}
}

func TestResolvePathCompletionContextAllowsPersistedHTTPOptIn(t *testing.T) {
	selected := profile.Profile{Name: "local", Address: "http://localhost:8200", Username: "local", AuthPath: "oidc", AllowInsecure: true}
	loader := &completionProfileLoader{configuration: config.Configuration{Profiles: []profile.Profile{selected}, ActiveProfile: selected.Name}}
	credentials := &completionCredentialGetter{token: "synthetic-token"}
	got, ok := resolvePathCompletionContext(context.Background(), "", CompletionDependencies{Profiles: loader, Credentials: credentials})
	if !ok || got.profile != selected || got.token != "synthetic-token" {
		t.Fatal("persisted HTTP opt-in did not permit the selected completion context")
	}
}
