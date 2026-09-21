package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"vlt/internal/config"
	"vlt/internal/profile"
)

type completionCredentialGetter struct {
	token string
	err   error
	names []string
}

type completionTestTransport struct{ handler http.Handler }

type completionCredentialGetFunc func(context.Context, string) (string, error)

func (get completionCredentialGetFunc) Get(ctx context.Context, name string) (string, error) {
	return get(ctx, name)
}

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
		case request.Method == "LIST" && request.URL.Path == "/v1/secret/team/folder/":
			listCalls.Add(1)
			_, _ = writer.Write([]byte(`{"data":{"keys":[]}}`))
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
	if listCalls.Load() != 2 || capabilityCalls.Load() != 1 {
		t.Errorf("list calls = %d, capability calls = %d; want two lists and one capability batch", listCalls.Load(), capabilityCalls.Load())
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
				case request.Method == "LIST" && request.URL.Path == "/v1/secret/metadata/team/folder/":
					listCalls.Add(1)
					_, _ = writer.Write([]byte(`{"data":{"keys":[]}}`))
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
			if listCalls.Load() != 2 || capabilityCalls.Load() != 1 {
				t.Errorf("list calls = %d, capability calls = %d; want two lists and one capability batch", listCalls.Load(), capabilityCalls.Load())
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

func TestCompletePathCandidatesBoundsBlockedCredentialLookup(t *testing.T) {
	selected := profile.Profile{Name: "team-a", Address: "https://vault.example.invalid", Username: "a", AuthPath: "oidc"}
	loader := &completionProfileLoader{configuration: config.Configuration{Profiles: []profile.Profile{selected}, ActiveProfile: selected.Name}}
	release := make(chan struct{})
	defer close(release)
	var requests atomic.Int32
	transport := completionTestTransport{handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) })}
	credentials := completionCredentialGetFunc(func(ctx context.Context, _ string) (string, error) {
		deadline, ok := ctx.Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining < 400*time.Millisecond || remaining > 550*time.Millisecond {
			t.Error("credential lookup did not share the 500 ms completion deadline")
		}
		<-release
		return "synthetic-token", nil
	})
	start := time.Now()
	got := completePathCandidates(context.Background(), CompletionDependencies{Profiles: loader, Credentials: credentials}, "", "read", "", "secret/a", transport)
	if len(got) != 0 || time.Since(start) > 800*time.Millisecond {
		t.Errorf("blocked credential lookup returned %q after %s", got, time.Since(start))
	}
	if requests.Load() != 0 {
		t.Error("expired credential lookup started a Vault request")
	}
}

func TestCompletePathCandidatesCancelsCapabilityCheckWithoutPartialPaths(t *testing.T) {
	selected := profile.Profile{Name: "team-a", Address: "https://vault.example.invalid", Username: "a", AuthPath: "oidc"}
	loader := &completionProfileLoader{configuration: config.Configuration{Profiles: []profile.Profile{selected}, ActiveProfile: selected.Name}}
	canceled := make(chan struct{})
	transport := completionTestTransport{handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/secret/":
			_, _ = writer.Write([]byte(`{"data":{"keys":["first","second"]}}`))
		case "/v1/sys/capabilities-self":
			<-request.Context().Done()
			close(canceled)
		default:
			t.Errorf("unexpected request %s", request.URL.Path)
		}
	})}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	got := completePathCandidates(ctx, CompletionDependencies{Profiles: loader, Credentials: &completionCredentialGetter{token: "synthetic-token"}}, "", "read", "", "secret/", transport)
	if len(got) != 0 {
		t.Errorf("canceled permission check returned %q", got)
	}
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Error("canceled permission check kept work running")
	}
}

func TestCompletePathCandidatesKeepsFailuresAndTokensOutOfOutput(t *testing.T) {
	const token = "hvs.synthetic-private-token"
	selected := profile.Profile{Name: "team-a", Address: "https://vault.example.invalid", Username: "a", AuthPath: "oidc"}
	loader := &completionProfileLoader{configuration: config.Configuration{Profiles: []profile.Profile{selected}, ActiveProfile: selected.Name}}
	for _, tt := range []struct {
		name        string
		credentials completionCredentialGetFunc
		listStatus  int
		listBody    string
	}{
		{name: "missing token", credentials: func(context.Context, string) (string, error) { return "", nil }},
		{name: "keyring error", credentials: func(context.Context, string) (string, error) {
			return "", errors.New("private keyring failure " + token)
		}},
		{name: "malformed listing", credentials: func(context.Context, string) (string, error) { return token, nil }, listBody: `{`},
		{name: "server error", credentials: func(context.Context, string) (string, error) { return token, nil }, listStatus: http.StatusInternalServerError, listBody: "private server failure " + token},
		{name: "token in listed name", credentials: func(context.Context, string) (string, error) { return token, nil }, listBody: `{"data":{"keys":["hvs.synthetic-private-token"]}}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			vault := &completionVaultStub{}
			transport := completionTestTransport{handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/v1/secret/":
					if tt.listStatus != 0 {
						writer.WriteHeader(tt.listStatus)
					}
					_, _ = writer.Write([]byte(tt.listBody))
				case "/v1/sys/capabilities-self":
					_, _ = writer.Write([]byte(`{"secret/hvs.synthetic-private-token":["read"]}`))
				default:
					t.Errorf("unexpected request %s", request.URL.Path)
				}
			})}
			got := completePathCandidates(context.Background(), CompletionDependencies{Profiles: loader, Credentials: tt.credentials, Vault: vault, Output: &output}, "", "read", "", "secret/", transport)
			if len(got) != 0 || output.Len() != 0 || vault.calls != 0 {
				t.Errorf("failed lookup returned %q, wrote %q, or called Vault %d times", got, output.String(), vault.calls)
			}
		})
	}
}

func TestCompletePathCandidatesReturnsOnlyCheckedReadPaths(t *testing.T) {
	selected := profile.Profile{Name: "team-a", Address: "https://vault.example.invalid", Username: "a", AuthPath: "oidc"}
	loader := &completionProfileLoader{configuration: config.Configuration{Profiles: []profile.Profile{selected}, ActiveProfile: selected.Name}}
	transport := completionTestTransport{handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/secret/":
			_, _ = writer.Write([]byte(`{"data":{"keys":["readable","hidden"]}}`))
		case "/v1/sys/capabilities-self":
			_, _ = writer.Write([]byte(`{"secret/readable":["read"],"secret/hidden":["deny"]}`))
		default:
			t.Errorf("unexpected request %s", request.URL.Path)
		}
	})}
	dependencies := CompletionDependencies{Profiles: loader, Credentials: &completionCredentialGetter{token: "synthetic-token"}}
	if got := completePathCandidates(context.Background(), dependencies, "", "read", "", "secret/", transport); !reflect.DeepEqual(got, []string{"secret/readable"}) {
		t.Errorf("checked path candidates = %q, want only readable path", got)
	}
	if got := completePathCandidates(context.Background(), dependencies, "", "write", "", "secret/", transport); len(got) != 0 {
		t.Errorf("unsupported command returned path candidates %q", got)
	}
}

func TestKVPathCheckpointRoutesV1AndKeepsTokenOutOfPathsAndBodies(t *testing.T) {
	const token = "hvs.synthetic-checkpoint-token"
	selected := profile.Profile{Name: "team-a", Address: "http://vault.example.invalid", Username: "a", AuthPath: "oidc", Namespace: "dept/", AllowInsecure: true}
	loader := &completionProfileLoader{configuration: config.Configuration{Profiles: []profile.Profile{selected}, ActiveProfile: selected.Name}}
	credentials := &completionCredentialGetter{token: token}
	completion, ok := resolvePathCompletionContext(context.Background(), "", CompletionDependencies{Profiles: loader, Credentials: credentials})
	if !ok {
		t.Fatal("selected KV v1 profile was unavailable")
	}
	var calls atomic.Int32
	transport := completionTestTransport{handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls.Add(1)
		body, err := io.ReadAll(request.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		if request.Header.Get("X-Vault-Token") != token || request.Header.Get("X-Vault-Namespace") != "dept/" {
			t.Error("KV v1 request lost the selected token or namespace")
		}
		if strings.Contains(request.URL.String(), token) || strings.Contains(string(body), token) {
			t.Error("KV path request placed the token outside its header")
		}
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v1/sys/internal/ui/mounts/secret/team/":
			_, _ = writer.Write([]byte(`{"path":"secret/","type":"kv","options":{}}`))
		case request.Method == "LIST" && request.URL.Path == "/v1/secret/team/":
			_, _ = writer.Write([]byte(`{"data":{"keys":["allowed","denied"]}}`))
		case request.Method == http.MethodPost && request.URL.Path == "/v1/sys/capabilities-self":
			var capabilityRequest struct {
				Paths []string `json:"paths"`
			}
			if err := json.Unmarshal(body, &capabilityRequest); err != nil {
				t.Errorf("decode capability body: %v", err)
			}
			if !reflect.DeepEqual(capabilityRequest.Paths, []string{"secret/team/allowed", "secret/team/denied"}) {
				t.Errorf("KV v1 read paths = %q, want the listed leaves", capabilityRequest.Paths)
			}
			_, _ = writer.Write([]byte(`{"secret/team/allowed":["read"],"secret/team/denied":["deny"]}`))
		default:
			t.Errorf("unexpected KV request: %s %s", request.Method, request.URL.Path)
			http.Error(writer, "unexpected request", http.StatusBadRequest)
		}
	})}
	got := completeKVPaths(context.Background(), completion, "", "secret/team/", transport)
	if !reflect.DeepEqual(got, []string{"secret/team/allowed"}) || strings.Contains(strings.Join(got, ""), token) {
		t.Errorf("KV v1 candidates are missing or contain the token")
	}
	if calls.Load() != 3 || !reflect.DeepEqual(credentials.names, []string{"team-a"}) {
		t.Errorf("requests = %d and credential reads = %q, want three requests and one read", calls.Load(), credentials.names)
	}
}

func TestCompleteReadPathsShowsOnlyListedReadableTargets(t *testing.T) {
	var listCalls, capabilityCalls atomic.Int32
	transport := completionTestTransport{handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == "LIST" && request.URL.Path == "/v1/identity/entity/id/":
			listCalls.Add(1)
			_, _ = writer.Write([]byte(`{"data":{"keys":["allowed","denied","folder/"]}}`))
		case request.Method == "LIST" && request.URL.Path == "/v1/identity/entity/id/folder/":
			listCalls.Add(1)
			_, _ = writer.Write([]byte(`{"data":{"keys":[]}}`))
		case request.Method == http.MethodPost && request.URL.Path == "/v1/sys/capabilities-self":
			capabilityCalls.Add(1)
			var body struct {
				Paths []string `json:"paths"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode capabilities: %v", err)
			}
			want := []string{"identity/entity/id/allowed", "identity/entity/id/denied"}
			if !reflect.DeepEqual(body.Paths, want) {
				t.Errorf("read capability paths = %q, want command target paths %q", body.Paths, want)
			}
			_, _ = writer.Write([]byte(`{"identity/entity/id/allowed":["read"],"identity/entity/id/denied":["deny"],"identity/entity/id/ghost":["read"]}`))
		default:
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
			http.Error(writer, "unexpected request", http.StatusBadRequest)
		}
	})}
	selected := pathCompletionContext{profile: profile.Profile{Address: "http://vault.example.invalid", AllowInsecure: true}, token: "synthetic-token"}
	got := completeReadPaths(context.Background(), selected, "identity/entity/id/", transport)
	if !reflect.DeepEqual(got, []string{"identity/entity/id/allowed"}) {
		t.Errorf("read candidates = %q, want only listed readable target", got)
	}
	if listCalls.Load() != 2 || capabilityCalls.Load() != 1 {
		t.Errorf("list calls = %d, capability calls = %d; want two lists and one capability batch", listCalls.Load(), capabilityCalls.Load())
	}
}

func TestCompleteReadPathsHidesUnsupportedOrDeniedBackends(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var calls atomic.Int32
			transport := completionTestTransport{handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				calls.Add(1)
				if request.Method != "LIST" || request.URL.Path != "/v1/aws/creds/" {
					t.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
				}
				writer.WriteHeader(status)
			})}
			selected := pathCompletionContext{profile: profile.Profile{Address: "http://vault.example.invalid", AllowInsecure: true}, token: "synthetic-token"}
			if got := completeReadPaths(context.Background(), selected, "aws/creds/", transport); len(got) != 0 {
				t.Errorf("unsupported or denied backend returned %q, want no candidates", got)
			}
			if calls.Load() != 1 {
				t.Errorf("calls = %d, want LIST only", calls.Load())
			}
		})
	}
}

func TestCompleteReadPathsProvesDeepFolderAndHidesUnreadableFolders(t *testing.T) {
	var listCalls, capabilityCalls atomic.Int32
	transport := completionTestTransport{handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == "LIST":
			listCalls.Add(1)
			switch request.URL.Path {
			case "/v1/secret/":
				_, _ = writer.Write([]byte(`{"data":{"keys":["direct","branch/","empty/","denied/"]}}`))
			case "/v1/secret/branch/":
				_, _ = writer.Write([]byte(`{"data":{"keys":["nested/"]}}`))
			case "/v1/secret/branch/nested/":
				_, _ = writer.Write([]byte(`{"data":{"keys":["allowed"]}}`))
			case "/v1/secret/empty/":
				_, _ = writer.Write([]byte(`{"data":{"keys":["nope"]}}`))
			case "/v1/secret/denied/":
				_, _ = writer.Write([]byte(`{"data":{"keys":["hidden"]}}`))
			default:
				t.Errorf("unexpected list path: %s", request.URL.Path)
				http.Error(writer, "unexpected list", http.StatusBadRequest)
			}
		case request.Method == http.MethodPost && request.URL.Path == "/v1/sys/capabilities-self":
			capabilityCalls.Add(1)
			_, _ = writer.Write([]byte(`{"secret/direct":["read"],"secret/branch/nested/allowed":["read"],"secret/empty/nope":["deny"],"secret/denied/hidden":["deny"]}`))
		default:
			t.Errorf("unexpected request: %s %s", request.Method, request.URL.Path)
			http.Error(writer, "unexpected request", http.StatusBadRequest)
		}
	})}
	selected := pathCompletionContext{profile: profile.Profile{Address: "http://vault.example.invalid", AllowInsecure: true}, token: "synthetic-token"}
	got := completeReadPaths(context.Background(), selected, "secret/", transport)
	want := []string{"secret/branch/", "secret/direct"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("folder candidates = %q, want %q", got, want)
	}
	if listCalls.Load() != 5 || capabilityCalls.Load() != 1 {
		t.Errorf("list calls = %d, capability calls = %d; want five lists and one batch", listCalls.Load(), capabilityCalls.Load())
	}
}

func TestCompleteReadPathsStopsFolderTraversalOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var capabilityCalls atomic.Int32
	transport := completionTestTransport{handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/secret/":
			_, _ = writer.Write([]byte(`{"data":{"keys":["branch/","direct"]}}`))
		case "/v1/secret/branch/":
			cancel()
			_, _ = writer.Write([]byte(`{"data":{"keys":["allowed"]}}`))
		case "/v1/sys/capabilities-self":
			capabilityCalls.Add(1)
		default:
			t.Errorf("unexpected request after cancellation: %s", request.URL.Path)
		}
	})}
	selected := pathCompletionContext{profile: profile.Profile{Address: "http://vault.example.invalid", AllowInsecure: true}, token: "synthetic-token"}
	if got := completeReadPaths(ctx, selected, "secret/", transport); len(got) != 0 {
		t.Errorf("canceled traversal returned %q, want no candidates", got)
	}
	if capabilityCalls.Load() != 0 {
		t.Error("canceled traversal queried capabilities")
	}
}

func TestCompleteReadPathsHidesFolderWhenDescendantsCannotBeChecked(t *testing.T) {
	transport := completionTestTransport{handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/secret/":
			_, _ = writer.Write([]byte(`{"data":{"keys":["unknown/"]}}`))
		case "/v1/secret/unknown/":
			writer.WriteHeader(http.StatusForbidden)
		default:
			t.Errorf("unexpected request after denied folder: %s", request.URL.Path)
		}
	})}
	selected := pathCompletionContext{profile: profile.Profile{Address: "http://vault.example.invalid", AllowInsecure: true}, token: "synthetic-token"}
	if got := completeReadPaths(context.Background(), selected, "secret/", transport); len(got) != 0 {
		t.Errorf("unchecked folder returned %q, want no candidates", got)
	}
}

func TestCompleteKVPathsProvesV2FolderThroughMetadataAndDataPaths(t *testing.T) {
	transport := completionTestTransport{handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/v1/sys/internal/ui/mounts/secret/":
			_, _ = writer.Write([]byte(`{"path":"secret/","type":"kv","options":{"version":"2"}}`))
		case request.Method == "LIST" && request.URL.Path == "/v1/secret/metadata/":
			_, _ = writer.Write([]byte(`{"data":{"keys":["branch/"]}}`))
		case request.Method == "LIST" && request.URL.Path == "/v1/secret/metadata/branch/":
			_, _ = writer.Write([]byte(`{"data":{"keys":["allowed"]}}`))
		case request.Method == http.MethodPost && request.URL.Path == "/v1/sys/capabilities-self":
			var body struct {
				Paths []string `json:"paths"`
			}
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Errorf("decode capabilities: %v", err)
			}
			if !reflect.DeepEqual(body.Paths, []string{"secret/data/branch/allowed"}) {
				t.Errorf("KV v2 folder read paths = %q, want data path", body.Paths)
			}
			_, _ = writer.Write([]byte(`{"secret/data/branch/allowed":["read"]}`))
		default:
			t.Errorf("unexpected KV v2 folder request: %s %s", request.Method, request.URL.Path)
			http.Error(writer, "unexpected request", http.StatusBadRequest)
		}
	})}
	selected := pathCompletionContext{profile: profile.Profile{Address: "http://vault.example.invalid", AllowInsecure: true}, token: "synthetic-token"}
	if got := completeKVPaths(context.Background(), selected, "", "secret/", transport); !reflect.DeepEqual(got, []string{"secret/branch/"}) {
		t.Errorf("KV v2 folder candidates = %q, want proven branch", got)
	}
}
