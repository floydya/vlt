package credential

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"vlt/internal/profile"
	"vlt/internal/vaultexec"
)

type fakeLoginExecutor struct {
	invocation vaultexec.Invocation
	result     vaultexec.Result
	err        error
	calls      int
}

func (f *fakeLoginExecutor) Execute(_ context.Context, invocation vaultexec.Invocation) (vaultexec.Result, error) {
	f.calls++
	f.invocation = invocation
	return f.result, f.err
}

type fakeLoginStore struct {
	profileName string
	token       string
	setErr      error
	setCalls    int
}

func (f *fakeLoginStore) Get(context.Context, string) (string, error) {
	return "", errors.New("unexpected Get call")
}

func (f *fakeLoginStore) Set(_ context.Context, profileName, token string) error {
	f.setCalls++
	f.profileName = profileName
	f.token = token
	return f.setErr
}

func (f *fakeLoginStore) Delete(context.Context, string) error {
	return errors.New("unexpected Delete call")
}

func loginTestProfile() profile.Profile {
	return profile.Profile{
		Name:      "team-a",
		Address:   "https://vault.example.com",
		Username:  "example-user",
		AuthPath:  "company-oidc",
		Namespace: "engineering",
	}
}

func TestAuthenticatorLoginInvokesVaultAndStoresClientToken(t *testing.T) {
	const token = "synthetic-login-token"
	executor := &fakeLoginExecutor{
		result: vaultexec.Result{Stdout: []byte(`{"auth":{"client_token":"` + token + `"}}`)},
	}
	store := &fakeLoginStore{}
	authenticator := NewAuthenticator(executor, store)

	err := authenticator.Login(context.Background(), loginTestProfile())
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	wantArguments := []string{
		"login",
		"-no-store",
		"-format=json",
		"-method=oidc",
		"-path=company-oidc",
		"username=example-user",
	}
	if !reflect.DeepEqual(executor.invocation.Arguments, wantArguments) {
		t.Errorf("login arguments = %#v, want %#v", executor.invocation.Arguments, wantArguments)
	}
	wantEnvironment := vaultexec.LoginEnvironment("https://vault.example.com", "engineering")
	if !reflect.DeepEqual(executor.invocation.Environment, wantEnvironment) {
		t.Errorf("login environment = %#v, want %#v", executor.invocation.Environment, wantEnvironment)
	}
	if executor.invocation.Mode != vaultexec.Captured {
		t.Errorf("login stream mode = %v, want Captured", executor.invocation.Mode)
	}
	if store.setCalls != 1 || store.profileName != "team-a" || store.token != token {
		t.Fatalf("store Set calls = %d, profile = %q, token = %q", store.setCalls, store.profileName, store.token)
	}
}

func TestAuthenticatorLoginRejectsHTTPWithoutOptInBeforeVaultOrStore(t *testing.T) {
	executor := &fakeLoginExecutor{}
	store := &fakeLoginStore{}
	selected := loginTestProfile()
	selected.Address = "http://vault.example.com"

	err := NewAuthenticator(executor, store).Login(context.Background(), selected)
	if err == nil || !strings.Contains(err.Error(), "allow-insecure") {
		t.Fatalf("Login() error = %v, want insecure transport diagnostic", err)
	}
	if executor.calls != 0 || store.setCalls != 0 {
		t.Fatalf("unsafe login calls: Vault=%d store=%d, want none", executor.calls, store.setCalls)
	}
}

func TestAuthenticatorLoginAllowsOptedInHTTP(t *testing.T) {
	executor := &fakeLoginExecutor{result: vaultexec.Result{Stdout: []byte(`{"auth":{"client_token":"synthetic-token"}}`)}}
	store := &fakeLoginStore{}
	selected := loginTestProfile()
	selected.Address = "http://127.0.0.1:8200"
	selected.AllowInsecure = true

	if err := NewAuthenticator(executor, store).Login(context.Background(), selected); err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if got, want := executor.invocation.Environment, vaultexec.LoginEnvironment(selected.Address, selected.Namespace); !reflect.DeepEqual(got, want) {
		t.Fatalf("login environment = %#v, want %#v", got, want)
	}
}

func TestAuthenticatorLoginRejectsMalformedOrMissingClientToken(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
	}{
		{name: "malformed JSON", stdout: `{"auth":`},
		{name: "missing auth", stdout: `{}`},
		{name: "missing token", stdout: `{"auth":{}}`},
		{name: "null token", stdout: `{"auth":{"client_token":null}}`},
		{name: "whitespace token", stdout: `{"auth":{"client_token":"   "}}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &fakeLoginExecutor{result: vaultexec.Result{Stdout: []byte(tt.stdout)}}
			store := &fakeLoginStore{}
			authenticator := NewAuthenticator(executor, store)

			err := authenticator.Login(context.Background(), loginTestProfile())
			if err == nil {
				t.Fatal("Login() error = nil, want authentication failure")
			}
			if store.setCalls != 0 {
				t.Fatalf("store Set calls = %d, want 0", store.setCalls)
			}
			if strings.Contains(err.Error(), tt.stdout) {
				t.Fatalf("Login() error exposed captured output: %q", err)
			}
		})
	}
}

func TestAuthenticatorLoginDoesNotStoreOnCancellationOrProcessFailure(t *testing.T) {
	const token = "opaque-login-failure-canary"
	tests := []struct {
		name      string
		err       error
		wantError error
	}{
		{name: "cancellation", err: context.Canceled, wantError: context.Canceled},
		{name: "process failure", err: errors.New("vault failed with " + token)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &fakeLoginExecutor{
				result: vaultexec.Result{Stdout: []byte(`{"auth":{"client_token":"` + token + `"}}`)},
				err:    tt.err,
			}
			store := &fakeLoginStore{}
			authenticator := NewAuthenticator(executor, store)

			err := authenticator.Login(context.Background(), loginTestProfile())
			if err == nil {
				t.Fatal("Login() error = nil, want failure")
			}
			if tt.wantError != nil && !errors.Is(err, tt.wantError) {
				t.Fatalf("Login() error = %v, want %v", err, tt.wantError)
			}
			if store.setCalls != 0 {
				t.Fatalf("store Set calls = %d, want 0", store.setCalls)
			}
			if strings.Contains(err.Error(), token) {
				t.Fatalf("Login() error exposed token: %q", err)
			}
		})
	}
}

func TestAuthenticatorLoginRedactsStoreFailure(t *testing.T) {
	const token = "opaque-store-failure-canary"
	executor := &fakeLoginExecutor{
		result: vaultexec.Result{Stdout: []byte(`{"auth":{"client_token":"` + token + `"}}`)},
	}
	store := &fakeLoginStore{setErr: errors.New("keyring rejected " + token)}
	authenticator := NewAuthenticator(executor, store)

	err := authenticator.Login(context.Background(), loginTestProfile())
	if err == nil {
		t.Fatal("Login() error = nil, want store failure")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("Login() error exposed token: %q", err)
	}
	if !strings.Contains(err.Error(), vaultexec.RedactedValue) {
		t.Fatalf("Login() error = %q, want redaction marker", err)
	}
}

func TestAuthenticatorLoginRejectsInvalidProfileBeforeExternalCalls(t *testing.T) {
	executor := &fakeLoginExecutor{}
	store := &fakeLoginStore{}
	authenticator := NewAuthenticator(executor, store)
	invalidProfile := loginTestProfile()
	invalidProfile.AuthPath = ""

	err := authenticator.Login(context.Background(), invalidProfile)
	if err == nil {
		t.Fatal("Login() error = nil, want invalid profile failure")
	}
	if executor.calls != 0 || store.setCalls != 0 {
		t.Fatalf("external calls: executor = %d, store = %d, want none", executor.calls, store.setCalls)
	}
}
