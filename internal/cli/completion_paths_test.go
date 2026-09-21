package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	"vlt/internal/config"
	"vlt/internal/profile"
)

type completionCredentialGetter struct {
	token string
	err   error
	names []string
}

type completionTestTransport struct{ handler http.Handler }

func (transport completionTestTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	transport.handler.ServeHTTP(recorder, request)
	return recorder.Result(), nil
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

func TestCompleteKVv1PathsChecksListedLeavesInOneCapabilityRequest(t *testing.T) {
	var listCalls, capabilityCalls atomic.Int32
	transport := completionTestTransport{handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Vault-Token") != "synthetic-token" || request.Header.Get("X-Vault-Namespace") != "dept/" {
			t.Error("path request did not use the selected token and namespace")
		}
		if request.URL.Host != "vault.example.invalid" || request.URL.RawQuery != "" {
			t.Error("path request used an unexpected address or query")
		}
		switch {
		case request.Method == "LIST" && request.URL.Path == "/v1/secret/team/":
			listCalls.Add(1)
			_, _ = writer.Write([]byte(`{"data":{"keys":["allowed","denied","other","folder/"]}}`))
		case request.Method == http.MethodPost && request.URL.Path == "/v1/sys/capabilities-self":
			capabilityCalls.Add(1)
			if request.Header.Get("Content-Type") != "application/json" {
				t.Error("capability request did not use JSON")
			}
			var body struct {
				Paths []string `json:"paths"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode capability request: %v", err)
			}
			want := []string{"secret/team/allowed", "secret/team/denied", "secret/team/other"}
			if !reflect.DeepEqual(body.Paths, want) {
				t.Errorf("capability paths = %q, want %q", body.Paths, want)
			}
			_, _ = writer.Write([]byte(`{"secret/team/allowed":["read"],"secret/team/denied":["read","deny"],"secret/team/other":["list"],"secret/team/ghost":["read"]}`))
		default:
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
			http.Error(writer, "unexpected request", http.StatusBadRequest)
		}
	})}
	selected := pathCompletionContext{profile: profile.Profile{Address: "http://vault.example.invalid", Namespace: "dept/", AllowInsecure: true}, token: "synthetic-token"}

	got := completeKVv1Paths(context.Background(), selected, "secret/team/", transport)
	if !reflect.DeepEqual(got, []string{"secret/team/allowed"}) {
		t.Errorf("KV v1 candidates = %q, want only the listed readable leaf", got)
	}
	if listCalls.Load() != 1 || capabilityCalls.Load() != 1 {
		t.Errorf("list calls = %d, capability calls = %d; want one each", listCalls.Load(), capabilityCalls.Load())
	}
}

func TestCompleteKVv1PathsRejectsHTTPWithoutOptInBeforeRequest(t *testing.T) {
	var requests atomic.Int32
	transport := completionTestTransport{handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests.Add(1)
	})}
	selected := pathCompletionContext{profile: profile.Profile{Address: "http://vault.example.invalid"}, token: "synthetic-token"}
	if got := completeKVv1Paths(context.Background(), selected, "secret/a", transport); len(got) != 0 {
		t.Errorf("unapproved HTTP returned %q, want no candidates", got)
	}
	if requests.Load() != 0 {
		t.Error("unapproved HTTP reached the request boundary")
	}
}

func TestCompleteKVv1PathsFailsClosedOnListOrCapabilityFailure(t *testing.T) {
	for _, tt := range []struct {
		name         string
		listStatus   int
		listBody     string
		capStatus    int
		capBody      string
		wantCapCalls int32
	}{
		{name: "denied list", listStatus: http.StatusForbidden},
		{name: "invalid list", listBody: `{"data":{"keys":"not-an-array"}}`},
		{name: "denied capabilities", listBody: `{"data":{"keys":["readable"]}}`, capStatus: http.StatusForbidden, wantCapCalls: 1},
		{name: "invalid capabilities", listBody: `{"data":{"keys":["readable"]}}`, capBody: `{`, wantCapCalls: 1},
		{name: "unverified capabilities", listBody: `{"data":{"keys":["readable"]}}`, capBody: `{}`, wantCapCalls: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var capabilityCalls atomic.Int32
			transport := completionTestTransport{handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/v1/secret/":
					if tt.listStatus != 0 {
						writer.WriteHeader(tt.listStatus)
					}
					_, _ = writer.Write([]byte(tt.listBody))
				case "/v1/sys/capabilities-self":
					capabilityCalls.Add(1)
					if tt.capStatus != 0 {
						writer.WriteHeader(tt.capStatus)
					}
					_, _ = writer.Write([]byte(tt.capBody))
				default:
					t.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
					http.Error(writer, "unexpected request", http.StatusBadRequest)
				}
			})}
			selected := pathCompletionContext{profile: profile.Profile{Address: "http://vault.example.invalid", AllowInsecure: true}, token: "synthetic-token"}
			if got := completeKVv1Paths(context.Background(), selected, "secret/r", transport); len(got) != 0 {
				t.Errorf("failed KV v1 lookup returned %q, want no candidates", got)
			}
			if capabilityCalls.Load() != tt.wantCapCalls {
				t.Errorf("capability calls = %d, want %d", capabilityCalls.Load(), tt.wantCapCalls)
			}
		})
	}
}

func TestCompleteKVv1PathsDoesNotFollowListRedirect(t *testing.T) {
	var redirected atomic.Int32
	transport := completionTestTransport{handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v1/secret/" {
			http.Redirect(writer, request, "/v1/secret/value", http.StatusFound)
			return
		}
		redirected.Add(1)
	})}
	selected := pathCompletionContext{profile: profile.Profile{Address: "http://vault.example.invalid", AllowInsecure: true}, token: "synthetic-token"}
	if got := completeKVv1Paths(context.Background(), selected, "secret/v", transport); len(got) != 0 {
		t.Errorf("redirected list returned %q, want no candidates", got)
	}
	if redirected.Load() != 0 {
		t.Error("path lookup followed a redirect toward a secret value")
	}
}

func TestCompleteKVv2PathsMapsMetadataListToDataReadForBothForms(t *testing.T) {
	for _, tt := range []struct {
		name       string
		mount      string
		prefix     string
		lookupPath string
		want       string
	}{
		{name: "combined path", prefix: "secret/team/", lookupPath: "/v1/sys/internal/ui/mounts/secret/team/", want: "secret/team/allowed"},
		{name: "mount flag", mount: "secret", prefix: "team/", lookupPath: "/v1/sys/internal/ui/mounts/secret", want: "team/allowed"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var listCalls, capabilityCalls atomic.Int32
			transport := completionTestTransport{handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Header.Get("X-Vault-Token") != "synthetic-token" || request.Header.Get("X-Vault-Namespace") != "dept/" {
					t.Error("KV v2 request did not use selected credentials and namespace")
				}
				switch {
				case request.Method == http.MethodGet && request.URL.Path == tt.lookupPath:
					_, _ = writer.Write([]byte(`{"path":"secret/","type":"kv","options":{"version":"2"}}`))
				case request.Method == "LIST" && request.URL.Path == "/v1/secret/metadata/team/":
					listCalls.Add(1)
					_, _ = writer.Write([]byte(`{"data":{"keys":["allowed","denied","folder/"]}}`))
				case request.Method == http.MethodPost && request.URL.Path == "/v1/sys/capabilities-self":
					capabilityCalls.Add(1)
					var body struct {
						Paths []string `json:"paths"`
					}
					if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
						t.Errorf("decode capability request: %v", err)
					}
					want := []string{"secret/data/team/allowed", "secret/data/team/denied"}
					if !reflect.DeepEqual(body.Paths, want) {
						t.Errorf("KV v2 capability paths = %q, want %q", body.Paths, want)
					}
					_, _ = writer.Write([]byte(`{"secret/data/team/allowed":["read"],"secret/data/team/denied":["deny"],"secret/metadata/team/denied":["read"]}`))
				default:
					t.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
					http.Error(writer, "unexpected request", http.StatusBadRequest)
				}
			})}
			selected := pathCompletionContext{profile: profile.Profile{Address: "http://vault.example.invalid", Namespace: "dept/", AllowInsecure: true}, token: "synthetic-token"}
			got := completeKVPaths(context.Background(), selected, tt.mount, tt.prefix, transport)
			if !reflect.DeepEqual(got, []string{tt.want}) {
				t.Errorf("KV v2 candidates = %q, want %q", got, tt.want)
			}
			if listCalls.Load() != 1 || capabilityCalls.Load() != 1 {
				t.Errorf("list calls = %d, capability calls = %d; want one each", listCalls.Load(), capabilityCalls.Load())
			}
		})
	}
}

func TestCompleteKVPathsFailsClosedWithoutConfirmedKVMount(t *testing.T) {
	for _, tt := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "mount unavailable", status: http.StatusForbidden},
		{name: "wrong engine", body: `{"path":"secret/","type":"transit","options":{"version":"2"}}`},
		{name: "unknown version", body: `{"path":"secret/","type":"kv","options":{"version":"3"}}`},
		{name: "wrong mount path", body: `{"path":"other/","type":"kv","options":{"version":"2"}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			transport := completionTestTransport{handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requests.Add(1)
				if request.URL.Path != "/v1/sys/internal/ui/mounts/secret/team/" {
					t.Errorf("unexpected path request: %s", request.URL.Path)
				}
				if tt.status != 0 {
					writer.WriteHeader(tt.status)
				}
				_, _ = writer.Write([]byte(tt.body))
			})}
			selected := pathCompletionContext{profile: profile.Profile{Address: "http://vault.example.invalid", AllowInsecure: true}, token: "synthetic-token"}
			if got := completeKVPaths(context.Background(), selected, "", "secret/team/", transport); len(got) != 0 {
				t.Errorf("unconfirmed KV mount returned %q, want no candidates", got)
			}
			if requests.Load() != 1 {
				t.Errorf("request count = %d, want mount lookup only", requests.Load())
			}
		})
	}
}
