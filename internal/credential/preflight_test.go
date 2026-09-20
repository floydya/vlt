package credential

import (
	"bytes"
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"vlt/internal/profile"
	"vlt/internal/vaultexec"
)

const preflightToken = "synthetic-preflight-token"

type preflightExecution struct {
	result       vaultexec.Result
	err          error
	afterExecute func()
}

type fakePreflightExecutor struct {
	executions  []preflightExecution
	invocations []vaultexec.Invocation
}

func (f *fakePreflightExecutor) Execute(_ context.Context, invocation vaultexec.Invocation) (vaultexec.Result, error) {
	f.invocations = append(f.invocations, invocation)
	if len(f.executions) == 0 {
		return vaultexec.Result{}, errors.New("unexpected Vault call")
	}
	execution := f.executions[0]
	f.executions = f.executions[1:]
	if execution.afterExecute != nil {
		execution.afterExecute()
	}
	return execution.result, execution.err
}

type fakePreflightRunner struct {
	result   vaultexec.Result
	err      error
	commands []vaultexec.Command
}

func (f *fakePreflightRunner) Run(_ context.Context, command vaultexec.Command) (vaultexec.Result, error) {
	f.commands = append(f.commands, command)
	return f.result, f.err
}

type fakePreflightStore struct {
	token    string
	getErr   error
	getCalls int
}

func (f *fakePreflightStore) Get(context.Context, string) (string, error) {
	f.getCalls++
	return f.token, f.getErr
}

func (f *fakePreflightStore) Set(_ context.Context, _ string, token string) error {
	f.token = token
	f.getErr = nil
	return nil
}

func (f *fakePreflightStore) Delete(context.Context, string) error {
	return errors.New("unexpected Delete call")
}

type fakePreflightAuthenticator struct {
	store *fakePreflightStore
	err   error
	calls int
}

func TestPreflightRejectsHTTPWithoutOptInBeforeCredentialOrVaultAccess(t *testing.T) {
	executor := &fakePreflightExecutor{}
	store := &fakePreflightStore{token: preflightToken}
	authenticator := &fakePreflightAuthenticator{store: store}
	selected := loginTestProfile()
	selected.Address = "http://vault.example.com"
	preflight := NewPreflight(executor, store, authenticator, time.Now, &bytes.Buffer{})

	_, err := preflight.Prepare(context.Background(), selected)
	if err == nil || !strings.Contains(err.Error(), "allow-insecure") {
		t.Fatalf("Prepare() error = %v, want insecure transport diagnostic", err)
	}
	if store.getCalls != 0 || len(executor.invocations) != 0 || authenticator.calls != 0 {
		t.Fatalf("unsafe preflight calls: store=%d Vault=%d login=%d, want none", store.getCalls, len(executor.invocations), authenticator.calls)
	}
}

func TestPreflightAllowsOptedInHTTP(t *testing.T) {
	now := time.Date(2030, time.January, 2, 15, 4, 5, 0, time.UTC)
	executor := &fakePreflightExecutor{executions: []preflightExecution{{
		result: vaultexec.Result{Stdout: []byte(`{"data":{"expire_time":"","renewable":false}}`)},
	}}}
	store := &fakePreflightStore{token: preflightToken}
	authenticator := &fakePreflightAuthenticator{store: store}
	selected := loginTestProfile()
	selected.Address = "http://127.0.0.1:8200"
	selected.AllowInsecure = true
	preflight := NewPreflight(executor, store, authenticator, func() time.Time { return now }, &bytes.Buffer{})

	token, err := preflight.Prepare(context.Background(), selected)
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if token != preflightToken {
		t.Fatalf("Prepare() token = %q, want stored token", token)
	}
	if got, want := executor.invocations[0].Environment, vaultexec.ProfileEnvironment(selected.Address, preflightToken, selected.Namespace); !reflect.DeepEqual(got, want) {
		t.Fatalf("preflight environment = %#v, want %#v", got, want)
	}
}

func (f *fakePreflightAuthenticator) Login(context.Context, profile.Profile) error {
	f.calls++
	if f.err == nil {
		f.store.token = "replacement-token"
		f.store.getErr = nil
	}
	return f.err
}

func TestPreflightMakesCredentialLifecycleDecisions(t *testing.T) {
	now := time.Date(2030, time.January, 2, 15, 4, 5, 0, time.UTC)
	valid := readPreflightFixture(t, "lookup-valid.json")
	expired := readPreflightFixture(t, "lookup-expired.json")
	lookupAt := func(expiry time.Time, renewable bool) []byte {
		return []byte(`{"data":{"expire_time":"` + expiry.Format(time.RFC3339Nano) + `","renewable":` + boolText(renewable) + `}}`)
	}

	tests := []struct {
		name          string
		storedToken   string
		storeErr      error
		executions    []preflightExecution
		clock         func() time.Time
		wantToken     string
		wantErr       string
		wantLogin     int
		wantArguments [][]string
		wantWarning   string
	}{
		{
			name:        "absent credential authenticates",
			storeErr:    ErrNotFound,
			clock:       func() time.Time { return now },
			wantToken:   "replacement-token",
			wantLogin:   1,
			wantWarning: "",
		},
		{
			name:        "valid credential is returned",
			storedToken: preflightToken,
			executions:  []preflightExecution{{result: vaultexec.Result{Stdout: valid}}},
			clock:       func() time.Time { return now },
			wantToken:   preflightToken,
			wantArguments: [][]string{
				{"token", "lookup", "-format=json"},
			},
		},
		{
			name:        "renewable credential at five minute boundary is renewed",
			storedToken: preflightToken,
			executions: []preflightExecution{
				{result: vaultexec.Result{Stdout: lookupAt(now.Add(5*time.Minute), true)}},
				{result: vaultexec.Result{Stdout: []byte(`{"auth":{"client_token":"rotated-response-token"}}`)}},
			},
			clock:     func() time.Time { return now },
			wantToken: preflightToken,
			wantArguments: [][]string{
				{"token", "lookup", "-format=json"},
				{"token", "renew", "-format=json"},
			},
		},
		{
			name:        "renewable credential outside five minute window is not renewed",
			storedToken: preflightToken,
			executions:  []preflightExecution{{result: vaultexec.Result{Stdout: lookupAt(now.Add(5*time.Minute+time.Second), true)}}},
			clock:       func() time.Time { return now },
			wantToken:   preflightToken,
			wantArguments: [][]string{
				{"token", "lookup", "-format=json"},
			},
		},
		{
			name:        "nonrenewable credential near expiry is returned",
			storedToken: preflightToken,
			executions:  []preflightExecution{{result: vaultexec.Result{Stdout: lookupAt(now.Add(time.Minute), false)}}},
			clock:       func() time.Time { return now },
			wantToken:   preflightToken,
			wantArguments: [][]string{
				{"token", "lookup", "-format=json"},
			},
		},
		{
			name:        "expired credential authenticates",
			storedToken: preflightToken,
			executions:  []preflightExecution{{result: vaultexec.Result{Stdout: expired}}},
			clock:       func() time.Time { return now },
			wantToken:   "replacement-token",
			wantLogin:   1,
			wantArguments: [][]string{
				{"token", "lookup", "-format=json"},
			},
		},
		{
			name:        "explicitly invalid credential authenticates",
			storedToken: preflightToken,
			executions: []preflightExecution{{
				result: vaultexec.Result{Stderr: []byte("Code: 403. Errors:\n\n* invalid token\n* permission denied")},
				err:    errors.New("Vault lookup failed"),
			}},
			clock:     func() time.Time { return now },
			wantToken: "replacement-token",
			wantLogin: 1,
			wantArguments: [][]string{
				{"token", "lookup", "-format=json"},
			},
		},
		{
			name:        "permission failure does not authenticate",
			storedToken: preflightToken,
			executions: []preflightExecution{{
				result: vaultexec.Result{Stderr: []byte("Code: 403. Errors:\n\n* permission denied")},
				err:    errors.New("Vault lookup failed"),
			}},
			clock:   func() time.Time { return now },
			wantErr: "validate credential",
			wantArguments: [][]string{
				{"token", "lookup", "-format=json"},
			},
		},
		{
			name:        "TLS failure does not authenticate",
			storedToken: preflightToken,
			executions:  []preflightExecution{{err: errors.New("x509: certificate has expired")}},
			clock:       func() time.Time { return now },
			wantErr:     "validate credential",
			wantArguments: [][]string{
				{"token", "lookup", "-format=json"},
			},
		},
		{
			name:        "malformed lookup does not authenticate",
			storedToken: preflightToken,
			executions:  []preflightExecution{{result: vaultexec.Result{Stdout: []byte(`{"data":`)}}},
			clock:       func() time.Time { return now },
			wantErr:     "malformed token lookup response",
			wantArguments: [][]string{
				{"token", "lookup", "-format=json"},
			},
		},
		{
			name:        "renewal failure keeps a credential that remains valid",
			storedToken: preflightToken,
			executions: []preflightExecution{
				{result: vaultexec.Result{Stdout: lookupAt(now.Add(time.Minute), true)}},
				{err: errors.New("renewal rejected token=" + preflightToken)},
			},
			clock:     func() time.Time { return now },
			wantToken: preflightToken,
			wantArguments: [][]string{
				{"token", "lookup", "-format=json"},
				{"token", "renew", "-format=json"},
			},
			wantWarning: "warning: Vault token renewal failed; continuing with the existing credential",
		},
		{
			name:        "renewal failure authenticates when the credential expires",
			storedToken: preflightToken,
			executions: []preflightExecution{
				{result: vaultexec.Result{Stdout: lookupAt(now.Add(time.Minute), true)}},
				{err: errors.New("renewal failed")},
			},
			clock:     sequentialClock(now, now.Add(2*time.Minute)),
			wantToken: "replacement-token",
			wantLogin: 1,
			wantArguments: [][]string{
				{"token", "lookup", "-format=json"},
				{"token", "renew", "-format=json"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakePreflightStore{token: tt.storedToken, getErr: tt.storeErr}
			executor := &fakePreflightExecutor{executions: append([]preflightExecution(nil), tt.executions...)}
			authenticator := &fakePreflightAuthenticator{store: store}
			var warnings bytes.Buffer
			preflight := NewPreflight(executor, store, authenticator, tt.clock, &warnings)

			token, err := preflight.Prepare(context.Background(), loginTestProfile())
			if tt.wantErr == "" && err != nil {
				t.Fatalf("Prepare() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("Prepare() error = %v, want text %q", err, tt.wantErr)
			}
			if token != tt.wantToken {
				t.Errorf("Prepare() token = %q, want %q", token, tt.wantToken)
			}
			if authenticator.calls != tt.wantLogin {
				t.Errorf("Login() calls = %d, want %d", authenticator.calls, tt.wantLogin)
			}
			if len(executor.executions) != 0 {
				t.Errorf("unused Vault executions = %d", len(executor.executions))
			}
			if got := invocationArguments(executor.invocations); !reflect.DeepEqual(got, tt.wantArguments) {
				t.Errorf("Vault arguments = %#v, want %#v", got, tt.wantArguments)
			}
			for _, invocation := range executor.invocations {
				wantEnvironment := vaultexec.ProfileEnvironment(loginTestProfile().Address, preflightToken, loginTestProfile().Namespace)
				if !reflect.DeepEqual(invocation.Environment, wantEnvironment) {
					t.Errorf("Vault environment = %#v, want %#v", invocation.Environment, wantEnvironment)
				}
				if invocation.Mode != vaultexec.Captured {
					t.Errorf("Vault stream mode = %v, want Captured", invocation.Mode)
				}
			}
			if tt.wantWarning == "" && warnings.Len() != 0 {
				t.Errorf("warning = %q, want none", warnings.String())
			}
			if tt.wantWarning != "" && !strings.Contains(warnings.String(), tt.wantWarning) {
				t.Errorf("warning = %q, want text %q", warnings.String(), tt.wantWarning)
			}
			if strings.Contains(warnings.String(), preflightToken) {
				t.Fatalf("warning exposed token: %q", warnings.String())
			}
		})
	}
}

func TestPreflightRejectsInvalidDependenciesAndInputsBeforeExternalCalls(t *testing.T) {
	now := func() time.Time { return time.Date(2030, time.January, 2, 15, 4, 5, 0, time.UTC) }

	tests := []struct {
		name      string
		build     func(*fakePreflightExecutor, *fakePreflightStore, *fakePreflightAuthenticator, *bytes.Buffer) *Preflight
		ctx       context.Context
		selected  profile.Profile
		wantError string
	}{
		{
			name: "nil receiver",
			build: func(*fakePreflightExecutor, *fakePreflightStore, *fakePreflightAuthenticator, *bytes.Buffer) *Preflight {
				return nil
			},
			ctx:       context.Background(),
			selected:  loginTestProfile(),
			wantError: "not configured",
		},
		{
			name: "canceled context",
			build: func(executor *fakePreflightExecutor, store *fakePreflightStore, authenticator *fakePreflightAuthenticator, warnings *bytes.Buffer) *Preflight {
				return NewPreflight(executor, store, authenticator, now, warnings)
			},
			ctx: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			}(),
			selected:  loginTestProfile(),
			wantError: context.Canceled.Error(),
		},
		{
			name: "invalid profile",
			build: func(executor *fakePreflightExecutor, store *fakePreflightStore, authenticator *fakePreflightAuthenticator, warnings *bytes.Buffer) *Preflight {
				return NewPreflight(executor, store, authenticator, now, warnings)
			},
			ctx: context.Background(),
			selected: func() profile.Profile {
				selected := loginTestProfile()
				selected.Name = ""
				return selected
			}(),
			wantError: "invalid profile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &fakePreflightExecutor{}
			store := &fakePreflightStore{token: preflightToken}
			authenticator := &fakePreflightAuthenticator{store: store}
			var warnings bytes.Buffer
			preflight := tt.build(executor, store, authenticator, &warnings)

			_, err := preflight.Prepare(tt.ctx, tt.selected)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("Prepare() error = %v, want text %q", err, tt.wantError)
			}
			if store.getCalls != 0 || authenticator.calls != 0 || len(executor.invocations) != 0 {
				t.Fatalf("external calls: store=%d login=%d Vault=%d, want none", store.getCalls, authenticator.calls, len(executor.invocations))
			}
		})
	}
}

func TestPreflightNetworkValidationFailureDoesNotAuthenticate(t *testing.T) {
	runner := &fakePreflightRunner{
		result: vaultexec.Result{Stderr: []byte("dial tcp: connection refused; VAULT_TOKEN=" + preflightToken)},
		err:    errors.New("network request failed"),
	}
	executor := vaultexec.NewExecutor(vaultexec.Dependencies{
		LookPath:    func(string) (string, error) { return "/test/vault", nil },
		Environment: func() []string { return nil },
		Runner:      runner,
	})
	store := &fakePreflightStore{token: preflightToken}
	authenticator := &fakePreflightAuthenticator{store: store}
	var warnings bytes.Buffer
	preflight := NewPreflight(
		executor,
		store,
		authenticator,
		func() time.Time { return time.Date(2030, time.January, 2, 15, 4, 5, 0, time.UTC) },
		&warnings,
	)

	_, err := preflight.Prepare(context.Background(), loginTestProfile())
	if err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("Prepare() error = %v, want network failure", err)
	}
	if strings.Contains(err.Error(), preflightToken) {
		t.Fatalf("Prepare() error exposed token: %q", err)
	}
	if authenticator.calls != 0 {
		t.Errorf("Login() calls = %d, want 0", authenticator.calls)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("Vault calls = %d, want 1", len(runner.commands))
	}
	if got, want := runner.commands[0].Arguments, []string{"token", "lookup", "-format=json"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Vault arguments = %#v, want %#v", got, want)
	}
	if strings.Contains(strings.Join(runner.commands[0].Arguments, " "), preflightToken) {
		t.Fatalf("Vault arguments exposed token: %#v", runner.commands[0].Arguments)
	}
	if warnings.Len() != 0 {
		t.Errorf("warning = %q, want none", warnings.String())
	}
}

func TestPreflightTreatsNegativeTTLAsExpired(t *testing.T) {
	store := &fakePreflightStore{token: preflightToken}
	executor := &fakePreflightExecutor{executions: []preflightExecution{{
		result: vaultexec.Result{Stdout: []byte(`{"data":{"renewable":false,"ttl":-1}}`)},
	}}}
	authenticator := &fakePreflightAuthenticator{store: store}
	preflight := NewPreflight(
		executor,
		store,
		authenticator,
		func() time.Time { return time.Date(2030, time.January, 2, 15, 4, 5, 0, time.UTC) },
		&bytes.Buffer{},
	)

	token, err := preflight.Prepare(context.Background(), loginTestProfile())
	if err != nil {
		t.Fatalf("Prepare() error = %v", err)
	}
	if token != "replacement-token" {
		t.Errorf("Prepare() token = %q, want replacement-token", token)
	}
	if authenticator.calls != 1 {
		t.Errorf("Login() calls = %d, want 1", authenticator.calls)
	}
}

func TestPreflightStopsWhenVaultExecutionCancelsContext(t *testing.T) {
	now := time.Date(2030, time.January, 2, 15, 4, 5, 0, time.UTC)
	lookup := []byte(`{"data":{"expire_time":"` + now.Add(time.Minute).Format(time.RFC3339Nano) + `","renewable":true}}`)

	tests := []struct {
		name       string
		executions func(context.CancelFunc) []preflightExecution
		wantCalls  int
	}{
		{
			name: "validation",
			executions: func(cancel context.CancelFunc) []preflightExecution {
				return []preflightExecution{{
					err:          errors.New("network request interrupted"),
					afterExecute: cancel,
				}}
			},
			wantCalls: 1,
		},
		{
			name: "renewal",
			executions: func(cancel context.CancelFunc) []preflightExecution {
				return []preflightExecution{
					{result: vaultexec.Result{Stdout: lookup}},
					{err: errors.New("renewal interrupted"), afterExecute: cancel},
				}
			},
			wantCalls: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			store := &fakePreflightStore{token: preflightToken}
			executor := &fakePreflightExecutor{executions: tt.executions(cancel)}
			authenticator := &fakePreflightAuthenticator{store: store}
			var warnings bytes.Buffer
			preflight := NewPreflight(executor, store, authenticator, func() time.Time { return now }, &warnings)

			token, err := preflight.Prepare(ctx, loginTestProfile())
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Prepare() error = %v, want context.Canceled", err)
			}
			if token != "" {
				t.Errorf("Prepare() token = %q, want empty", token)
			}
			if authenticator.calls != 0 {
				t.Errorf("Login() calls = %d, want 0", authenticator.calls)
			}
			if len(executor.invocations) != tt.wantCalls {
				t.Errorf("Vault calls = %d, want %d", len(executor.invocations), tt.wantCalls)
			}
			if warnings.Len() != 0 {
				t.Errorf("warning = %q, want none", warnings.String())
			}
		})
	}
}

func TestPreflightRejectsTTLThatCannotFitInDurationWithoutAuthenticating(t *testing.T) {
	store := &fakePreflightStore{token: preflightToken}
	executor := &fakePreflightExecutor{executions: []preflightExecution{{
		result: vaultexec.Result{Stdout: []byte(`{"data":{"renewable":true,"ttl":9223372036854775807}}`)},
	}}}
	authenticator := &fakePreflightAuthenticator{store: store}
	preflight := NewPreflight(
		executor,
		store,
		authenticator,
		func() time.Time { return time.Date(2030, time.January, 2, 15, 4, 5, 0, time.UTC) },
		&bytes.Buffer{},
	)

	_, err := preflight.Prepare(context.Background(), loginTestProfile())
	if err == nil || !strings.Contains(err.Error(), "malformed token TTL") {
		t.Fatalf("Prepare() error = %v, want malformed token TTL", err)
	}
	if authenticator.calls != 0 {
		t.Errorf("Login() calls = %d, want 0", authenticator.calls)
	}
}

func readPreflightFixture(t *testing.T, name string) []byte {
	t.Helper()
	contents, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %q: %v", name, err)
	}
	return contents
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func sequentialClock(values ...time.Time) func() time.Time {
	index := 0
	return func() time.Time {
		if index >= len(values) {
			return values[len(values)-1]
		}
		value := values[index]
		index++
		return value
	}
}

func invocationArguments(invocations []vaultexec.Invocation) [][]string {
	if len(invocations) == 0 {
		return nil
	}
	arguments := make([][]string, len(invocations))
	for index, invocation := range invocations {
		arguments[index] = invocation.Arguments
	}
	return arguments
}
