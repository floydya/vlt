package profile

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

type mutationConfigStore struct {
	configuration Configuration
	loadErr       error
	loadCalls     int
	saveCalls     int
	saveErrAt     int
	saveAfterErr  bool
}

func (s *mutationConfigStore) Load(context.Context) (Configuration, error) {
	s.loadCalls++
	if s.loadErr != nil {
		return Configuration{}, s.loadErr
	}
	return cloneConfiguration(s.configuration), nil
}

func (s *mutationConfigStore) Save(_ context.Context, configuration Configuration) error {
	s.saveCalls++
	if s.saveErrAt == s.saveCalls && !s.saveAfterErr {
		return errors.New("injected configuration failure")
	}
	s.configuration = cloneConfiguration(configuration)
	if s.saveErrAt == s.saveCalls {
		return errors.New("injected configuration failure after replacement")
	}
	return nil
}

type mutationCredentialStore struct {
	credentials map[string]string
	getErr      error
	setErr      error
	deleteErr   error
	deleteAfter bool
	getCalls    int
	setCalls    int
	deleteCalls int
}

func newMutationCredentialStore() *mutationCredentialStore {
	return &mutationCredentialStore{credentials: make(map[string]string)}
}

func (s *mutationCredentialStore) Get(_ context.Context, name string) (string, error) {
	s.getCalls++
	if s.getErr != nil {
		return "", s.getErr
	}
	token, found := s.credentials[name]
	if !found {
		return "", ErrCredentialNotFound
	}
	return token, nil
}

func (s *mutationCredentialStore) Set(_ context.Context, name, token string) error {
	s.setCalls++
	if s.setErr != nil {
		return s.setErr
	}
	s.credentials[name] = token
	return nil
}

func (s *mutationCredentialStore) Delete(_ context.Context, name string) error {
	s.deleteCalls++
	if s.deleteErr != nil && !s.deleteAfter {
		return s.deleteErr
	}
	if _, found := s.credentials[name]; !found {
		return ErrCredentialNotFound
	}
	delete(s.credentials, name)
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return nil
}

type mutationAuthenticator struct {
	credentials *mutationCredentialStore
	token       string
	err         error
	writeOnErr  bool
	calls       []Profile
}

func (a *mutationAuthenticator) Login(_ context.Context, selected Profile) error {
	a.calls = append(a.calls, selected)
	if a.err == nil || a.writeOnErr {
		a.credentials.credentials[selected.Name] = a.token
	}
	return a.err
}

func mutationTestProfile(name string) Profile {
	return Profile{
		Name:      name,
		Address:   "https://vault.example.com/" + name,
		Username:  "user-" + name,
		AuthPath:  "oidc",
		Namespace: "engineering/" + name,
	}
}

func TestMutationServiceAddPersistsProfileAndAuthenticates(t *testing.T) {
	configuration := Configuration{
		Profiles:      []Profile{mutationTestProfile("team-a")},
		ActiveProfile: "team-a",
	}
	configurations := &mutationConfigStore{configuration: configuration}
	credentials := newMutationCredentialStore()
	authenticator := &mutationAuthenticator{credentials: credentials, token: "synthetic-team-b-token"}
	service := NewMutationService(configurations, credentials, authenticator)
	added := mutationTestProfile("team-b")

	if err := service.Add(context.Background(), added); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	want := Configuration{
		Profiles:      []Profile{mutationTestProfile("team-a"), added},
		ActiveProfile: "team-a",
	}
	if !reflect.DeepEqual(configurations.configuration, want) {
		t.Fatalf("configuration after Add() = %#v, want %#v", configurations.configuration, want)
	}
	if got := credentials.credentials["team-b"]; got != "synthetic-team-b-token" {
		t.Fatalf("credential after Add() = %q, want authenticated token", got)
	}
	if !reflect.DeepEqual(authenticator.calls, []Profile{added}) {
		t.Fatalf("Login() profiles = %#v, want added profile", authenticator.calls)
	}
}

func TestMutationServiceAddRejectsDuplicateWithoutChangingState(t *testing.T) {
	original := Configuration{
		Profiles:      []Profile{mutationTestProfile("team-a")},
		ActiveProfile: "team-a",
	}
	configurations := &mutationConfigStore{configuration: original}
	credentials := newMutationCredentialStore()
	credentials.credentials["team-a"] = "original-token"
	authenticator := &mutationAuthenticator{credentials: credentials, token: "replacement-token"}
	service := NewMutationService(configurations, credentials, authenticator)

	err := service.Add(context.Background(), mutationTestProfile("team-a"))
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("Add() error = %v, want duplicate error", err)
	}
	if !reflect.DeepEqual(configurations.configuration, original) {
		t.Fatalf("configuration after duplicate Add() = %#v, want %#v", configurations.configuration, original)
	}
	if got := credentials.credentials["team-a"]; got != "original-token" {
		t.Fatalf("credential after duplicate Add() = %q, want original token", got)
	}
	if configurations.saveCalls != 0 || len(authenticator.calls) != 0 || credentials.deleteCalls != 0 {
		t.Fatalf("duplicate Add() calls: save = %d, login = %d, delete = %d; want none", configurations.saveCalls, len(authenticator.calls), credentials.deleteCalls)
	}
}

func TestMutationServiceAddRejectsInvalidProfileBeforeLoadingState(t *testing.T) {
	configurations := &mutationConfigStore{loadErr: errors.New("load must not run")}
	credentials := newMutationCredentialStore()
	authenticator := &mutationAuthenticator{credentials: credentials}
	service := NewMutationService(configurations, credentials, authenticator)
	invalid := mutationTestProfile("team-a")
	invalid.Address = "vault.example.com"

	err := service.Add(context.Background(), invalid)
	if err == nil || !strings.Contains(err.Error(), "invalid profile") {
		t.Fatalf("Add() error = %v, want validation error", err)
	}
	if configurations.saveCalls != 0 || len(authenticator.calls) != 0 || credentials.deleteCalls != 0 {
		t.Fatal("invalid Add() reached an external mutation boundary")
	}
}

func TestMutationServiceAddRejectsHTTPWithoutOptInBeforeLoadingState(t *testing.T) {
	configurations := &mutationConfigStore{loadErr: errors.New("load must not run")}
	credentials := newMutationCredentialStore()
	authenticator := &mutationAuthenticator{credentials: credentials}
	service := NewMutationService(configurations, credentials, authenticator)
	candidate := mutationTestProfile("team-a")
	candidate.Address = "http://vault.example.com"

	err := service.Add(context.Background(), candidate)
	if err == nil || !strings.Contains(err.Error(), "allow-insecure") {
		t.Fatalf("Add() error = %v, want insecure transport diagnostic", err)
	}
	if configurations.loadCalls != 0 || configurations.saveCalls != 0 || len(authenticator.calls) != 0 {
		t.Fatal("insecure Add() reached an external boundary")
	}
}

func TestMutationServiceAddPersistsHTTPOptIn(t *testing.T) {
	configurations := &mutationConfigStore{}
	credentials := newMutationCredentialStore()
	authenticator := &mutationAuthenticator{credentials: credentials, token: "synthetic-token"}
	service := NewMutationService(configurations, credentials, authenticator)
	candidate := mutationTestProfile("local")
	candidate.Address = "http://127.0.0.1:8200"
	candidate.AllowInsecure = true

	if err := service.Add(context.Background(), candidate); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if got := configurations.configuration.Profiles[0]; got != candidate {
		t.Fatalf("persisted profile = %#v, want %#v", got, candidate)
	}
}

func TestMutationServiceAddRestoresStateAfterConfigurationFailure(t *testing.T) {
	original := Configuration{
		Profiles:      []Profile{mutationTestProfile("team-a")},
		ActiveProfile: "team-a",
	}
	configurations := &mutationConfigStore{
		configuration: original,
		saveErrAt:     1,
		saveAfterErr:  true,
	}
	credentials := newMutationCredentialStore()
	authenticator := &mutationAuthenticator{credentials: credentials, token: "unused-token"}
	service := NewMutationService(configurations, credentials, authenticator)

	err := service.Add(context.Background(), mutationTestProfile("team-b"))
	if err == nil || !strings.Contains(err.Error(), "persist profile") {
		t.Fatalf("Add() error = %v, want persistence error", err)
	}
	if !reflect.DeepEqual(configurations.configuration, original) {
		t.Fatalf("configuration after failed Add() = %#v, want original %#v", configurations.configuration, original)
	}
	if len(authenticator.calls) != 0 {
		t.Fatalf("Login() calls = %d, want 0", len(authenticator.calls))
	}
	if _, found := credentials.credentials["team-b"]; found {
		t.Fatal("credential remained after failed profile persistence")
	}
}

func TestMutationServiceAddRestoresStateAfterAuthenticationFailure(t *testing.T) {
	for _, writeOnErr := range []bool{false, true} {
		name := "before credential write"
		if writeOnErr {
			name = "after credential write"
		}
		t.Run(name, func(t *testing.T) {
			original := Configuration{
				Profiles:      []Profile{mutationTestProfile("team-a")},
				ActiveProfile: "team-a",
			}
			configurations := &mutationConfigStore{configuration: original}
			credentials := newMutationCredentialStore()
			authenticator := &mutationAuthenticator{
				credentials: credentials,
				token:       "partial-token",
				err:         errors.New("injected authentication failure"),
				writeOnErr:  writeOnErr,
			}
			service := NewMutationService(configurations, credentials, authenticator)

			err := service.Add(context.Background(), mutationTestProfile("team-b"))
			if err == nil || !strings.Contains(err.Error(), "authenticate profile") {
				t.Fatalf("Add() error = %v, want authentication error", err)
			}
			if !reflect.DeepEqual(configurations.configuration, original) {
				t.Fatalf("configuration after failed Add() = %#v, want original %#v", configurations.configuration, original)
			}
			if _, found := credentials.credentials["team-b"]; found {
				t.Fatal("credential remained after failed authentication")
			}
		})
	}
}

func TestMutationServiceAddReportsIncompleteCleanup(t *testing.T) {
	configurations := &mutationConfigStore{}
	credentials := newMutationCredentialStore()
	credentials.deleteErr = errors.New("credential store unavailable")
	authenticator := &mutationAuthenticator{
		credentials: credentials,
		token:       "partial-token",
		err:         errors.New("injected authentication failure"),
		writeOnErr:  true,
	}
	service := NewMutationService(configurations, credentials, authenticator)

	err := service.Add(context.Background(), mutationTestProfile("team-a"))
	if err == nil {
		t.Fatal("Add() error = nil, want cleanup failure")
	}
	for _, want := range []string{"authenticate profile", "remove incomplete credential"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Add() error = %q, want %q", err, want)
		}
	}
	if len(configurations.configuration.Profiles) != 0 {
		t.Fatalf("profiles after partial cleanup = %#v, want none", configurations.configuration.Profiles)
	}
}

func TestMutationServiceUpdateChangesOnlySuppliedFieldsAndReauthenticates(t *testing.T) {
	originalProfile := mutationTestProfile("team-a")
	original := Configuration{Profiles: []Profile{originalProfile}, ActiveProfile: "team-a"}
	configurations := &mutationConfigStore{configuration: original}
	credentials := newMutationCredentialStore()
	credentials.credentials["team-a"] = "original-token"
	authenticator := &mutationAuthenticator{credentials: credentials, token: "replacement-token"}
	service := NewMutationService(configurations, credentials, authenticator)
	address := "https://vault-new.example.com"
	namespace := "new-namespace"

	err := service.Update(context.Background(), "team-a", ProfileChanges{
		Address:   &address,
		Namespace: &namespace,
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	wantProfile := originalProfile
	wantProfile.Address = address
	wantProfile.Namespace = namespace
	want := Configuration{Profiles: []Profile{wantProfile}, ActiveProfile: "team-a"}
	if !reflect.DeepEqual(configurations.configuration, want) {
		t.Fatalf("configuration after Update() = %#v, want %#v", configurations.configuration, want)
	}
	if got := credentials.credentials["team-a"]; got != "replacement-token" {
		t.Fatalf("credential after Update() = %q, want replacement token", got)
	}
	if !reflect.DeepEqual(authenticator.calls, []Profile{wantProfile}) {
		t.Fatalf("Login() profiles = %#v, want updated profile", authenticator.calls)
	}
}

func TestMutationServiceUpdateCanEnableAndClearHTTPOptIn(t *testing.T) {
	for _, tt := range []struct {
		name            string
		originalAddress string
		originalAllowed bool
		updatedAddress  string
		updatedAllowed  bool
	}{
		{
			name:            "enable with HTTP address",
			originalAddress: "https://vault.example.com",
			updatedAddress:  "http://vault.example.com",
			updatedAllowed:  true,
		},
		{
			name:            "clear with HTTPS address",
			originalAddress: "http://vault.example.com",
			originalAllowed: true,
			updatedAddress:  "https://vault.example.com",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			originalProfile := mutationTestProfile("team-a")
			originalProfile.Address = tt.originalAddress
			originalProfile.AllowInsecure = tt.originalAllowed
			configurations := &mutationConfigStore{configuration: Configuration{Profiles: []Profile{originalProfile}}}
			credentials := newMutationCredentialStore()
			credentials.credentials["team-a"] = "original-token"
			authenticator := &mutationAuthenticator{credentials: credentials, token: "replacement-token"}
			service := NewMutationService(configurations, credentials, authenticator)

			err := service.Update(context.Background(), "team-a", ProfileChanges{
				Address:       &tt.updatedAddress,
				AllowInsecure: &tt.updatedAllowed,
			})
			if err != nil {
				t.Fatalf("Update() error = %v", err)
			}
			got := configurations.configuration.Profiles[0]
			if got.Address != tt.updatedAddress || got.AllowInsecure != tt.updatedAllowed {
				t.Fatalf("updated profile = %#v, want address %q and allow_insecure %t", got, tt.updatedAddress, tt.updatedAllowed)
			}
		})
	}
}

func TestMutationServiceUpdateRejectsHTTPWithoutOptInBeforeCredentialAccess(t *testing.T) {
	original := Configuration{Profiles: []Profile{mutationTestProfile("team-a")}}
	configurations := &mutationConfigStore{configuration: original}
	credentials := newMutationCredentialStore()
	authenticator := &mutationAuthenticator{credentials: credentials}
	service := NewMutationService(configurations, credentials, authenticator)
	address := "http://vault.example.com"

	err := service.Update(context.Background(), "team-a", ProfileChanges{Address: &address})
	if err == nil || !strings.Contains(err.Error(), "allow-insecure") {
		t.Fatalf("Update() error = %v, want insecure transport diagnostic", err)
	}
	if credentials.getCalls != 0 || configurations.saveCalls != 0 || len(authenticator.calls) != 0 {
		t.Fatal("insecure Update() reached a mutation boundary")
	}
}

func TestMutationServiceUpdateWithNoChangesDoesNotMutateState(t *testing.T) {
	original := Configuration{Profiles: []Profile{mutationTestProfile("team-a")}, ActiveProfile: "team-a"}
	configurations := &mutationConfigStore{configuration: original}
	credentials := newMutationCredentialStore()
	credentials.credentials["team-a"] = "original-token"
	authenticator := &mutationAuthenticator{credentials: credentials, token: "replacement-token"}
	service := NewMutationService(configurations, credentials, authenticator)

	if err := service.Update(context.Background(), "team-a", ProfileChanges{}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if !reflect.DeepEqual(configurations.configuration, original) {
		t.Fatalf("configuration after no-op Update() = %#v, want %#v", configurations.configuration, original)
	}
	if configurations.saveCalls != 0 || credentials.getCalls != 0 || len(authenticator.calls) != 0 {
		t.Fatalf("no-op Update() calls: save = %d, get credential = %d, login = %d; want none", configurations.saveCalls, credentials.getCalls, len(authenticator.calls))
	}
}

func TestMutationServiceUpdateRejectsMissingOrInvalidProfileWithoutChangingState(t *testing.T) {
	tests := []struct {
		name    string
		profile string
		changes ProfileChanges
		wantErr string
	}{
		{
			name:    "missing profile",
			profile: "missing",
			changes: ProfileChanges{Address: stringPointer("https://new.example.com")},
			wantErr: "not found",
		},
		{
			name:    "invalid update",
			profile: "team-a",
			changes: ProfileChanges{Username: stringPointer("   ")},
			wantErr: "invalid profile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := Configuration{Profiles: []Profile{mutationTestProfile("team-a")}, ActiveProfile: "team-a"}
			configurations := &mutationConfigStore{configuration: original}
			credentials := newMutationCredentialStore()
			credentials.credentials["team-a"] = "original-token"
			authenticator := &mutationAuthenticator{credentials: credentials, token: "replacement-token"}
			service := NewMutationService(configurations, credentials, authenticator)

			err := service.Update(context.Background(), tt.profile, tt.changes)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Update() error = %v, want %q", err, tt.wantErr)
			}
			if !reflect.DeepEqual(configurations.configuration, original) {
				t.Fatalf("configuration after rejected Update() = %#v, want %#v", configurations.configuration, original)
			}
			if configurations.saveCalls != 0 || credentials.getCalls != 0 || len(authenticator.calls) != 0 {
				t.Fatal("rejected Update() reached a mutation boundary")
			}
		})
	}
}

func TestMutationServiceUpdateRestoresOriginalStateAfterAuthenticationFailure(t *testing.T) {
	tests := []struct {
		name          string
		originalToken string
		writeOnErr    bool
	}{
		{name: "existing token before credential write", originalToken: "original-token"},
		{name: "existing token after credential write", originalToken: "original-token", writeOnErr: true},
		{name: "missing token after credential write", writeOnErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := Configuration{Profiles: []Profile{mutationTestProfile("team-a")}, ActiveProfile: "team-a"}
			configurations := &mutationConfigStore{configuration: original}
			credentials := newMutationCredentialStore()
			if tt.originalToken != "" {
				credentials.credentials["team-a"] = tt.originalToken
			}
			authenticator := &mutationAuthenticator{
				credentials: credentials,
				token:       "partial-token",
				err:         errors.New("injected authentication failure"),
				writeOnErr:  tt.writeOnErr,
			}
			service := NewMutationService(configurations, credentials, authenticator)

			err := service.Update(context.Background(), "team-a", ProfileChanges{Address: stringPointer("https://new.example.com")})
			if err == nil || !strings.Contains(err.Error(), "authenticate profile") {
				t.Fatalf("Update() error = %v, want authentication error", err)
			}
			if !reflect.DeepEqual(configurations.configuration, original) {
				t.Fatalf("configuration after failed Update() = %#v, want %#v", configurations.configuration, original)
			}
			got, found := credentials.credentials["team-a"]
			if tt.originalToken == "" && found {
				t.Fatalf("credential after failed Update() = %q, want absent", got)
			}
			if tt.originalToken != "" && got != tt.originalToken {
				t.Fatalf("credential after failed Update() = %q, want %q", got, tt.originalToken)
			}
		})
	}
}

func TestMutationServiceUpdateRestoresOriginalStateAfterConfigurationFailure(t *testing.T) {
	original := Configuration{Profiles: []Profile{mutationTestProfile("team-a")}, ActiveProfile: "team-a"}
	configurations := &mutationConfigStore{
		configuration: original,
		saveErrAt:     1,
		saveAfterErr:  true,
	}
	credentials := newMutationCredentialStore()
	credentials.credentials["team-a"] = "original-token"
	authenticator := &mutationAuthenticator{credentials: credentials, token: "replacement-token"}
	service := NewMutationService(configurations, credentials, authenticator)

	err := service.Update(context.Background(), "team-a", ProfileChanges{Address: stringPointer("https://new.example.com")})
	if err == nil || !strings.Contains(err.Error(), "persist updated profile") {
		t.Fatalf("Update() error = %v, want persistence error", err)
	}
	if !reflect.DeepEqual(configurations.configuration, original) {
		t.Fatalf("configuration after failed Update() = %#v, want %#v", configurations.configuration, original)
	}
	if got := credentials.credentials["team-a"]; got != "original-token" {
		t.Fatalf("credential after failed Update() = %q, want original token", got)
	}
}

func TestMutationServiceUpdateStopsWhenOriginalCredentialCannotBeRead(t *testing.T) {
	original := Configuration{Profiles: []Profile{mutationTestProfile("team-a")}, ActiveProfile: "team-a"}
	configurations := &mutationConfigStore{configuration: original}
	credentials := newMutationCredentialStore()
	credentials.getErr = errors.New("credential store unavailable")
	authenticator := &mutationAuthenticator{credentials: credentials, token: "replacement-token"}
	service := NewMutationService(configurations, credentials, authenticator)

	err := service.Update(context.Background(), "team-a", ProfileChanges{Address: stringPointer("https://new.example.com")})
	if err == nil || !strings.Contains(err.Error(), "load original credential") {
		t.Fatalf("Update() error = %v, want credential load error", err)
	}
	if !reflect.DeepEqual(configurations.configuration, original) || len(authenticator.calls) != 0 || configurations.saveCalls != 0 {
		t.Fatal("failed credential snapshot changed update state")
	}
}

func TestMutationServiceUpdateReportsIncompleteCredentialRestore(t *testing.T) {
	original := Configuration{Profiles: []Profile{mutationTestProfile("team-a")}}
	configurations := &mutationConfigStore{configuration: original}
	credentials := newMutationCredentialStore()
	credentials.credentials["team-a"] = "original-token"
	credentials.setErr = errors.New("credential store unavailable")
	authenticator := &mutationAuthenticator{
		credentials: credentials,
		token:       "partial-token",
		err:         errors.New("injected authentication failure"),
		writeOnErr:  true,
	}
	service := NewMutationService(configurations, credentials, authenticator)

	err := service.Update(context.Background(), "team-a", ProfileChanges{Address: stringPointer("https://new.example.com")})
	if err == nil {
		t.Fatal("Update() error = nil, want restore failure")
	}
	for _, want := range []string{"authenticate profile", "restore original credential"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Update() error = %q, want %q", err, want)
		}
	}
}

func stringPointer(value string) *string {
	return &value
}

func TestMutationServiceRemoveDeletesProfileAndCredential(t *testing.T) {
	tests := []struct {
		name       string
		active     string
		wantActive string
	}{
		{name: "active profile", active: "team-a"},
		{name: "inactive profile", active: "team-b", wantActive: "team-b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configurations := &mutationConfigStore{configuration: Configuration{
				Profiles:      []Profile{mutationTestProfile("team-a"), mutationTestProfile("team-b")},
				ActiveProfile: tt.active,
			}}
			credentials := newMutationCredentialStore()
			credentials.credentials["team-a"] = "team-a-token"
			credentials.credentials["team-b"] = "team-b-token"
			authenticator := &mutationAuthenticator{credentials: credentials}
			service := NewMutationService(configurations, credentials, authenticator)

			if err := service.Remove(context.Background(), "team-a"); err != nil {
				t.Fatalf("Remove() error = %v", err)
			}
			want := Configuration{Profiles: []Profile{mutationTestProfile("team-b")}, ActiveProfile: tt.wantActive}
			if !reflect.DeepEqual(configurations.configuration, want) {
				t.Fatalf("configuration after Remove() = %#v, want %#v", configurations.configuration, want)
			}
			if _, found := credentials.credentials["team-a"]; found {
				t.Fatal("removed profile credential still exists")
			}
			if got := credentials.credentials["team-b"]; got != "team-b-token" {
				t.Fatalf("other profile credential = %q, want unchanged", got)
			}
		})
	}
}

func TestMutationServiceRemoveSucceedsWhenCredentialIsAlreadyAbsent(t *testing.T) {
	configurations := &mutationConfigStore{configuration: Configuration{
		Profiles:      []Profile{mutationTestProfile("team-a")},
		ActiveProfile: "team-a",
	}}
	credentials := newMutationCredentialStore()
	service := NewMutationService(configurations, credentials, nil)

	if err := service.Remove(context.Background(), "team-a"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if len(configurations.configuration.Profiles) != 0 || configurations.configuration.ActiveProfile != "" {
		t.Fatalf("configuration after Remove() = %#v, want empty", configurations.configuration)
	}
}

func TestMutationServiceRejectsCanceledContextBeforeExternalCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, tt := range []struct {
		name   string
		invoke func(*MutationService) error
	}{
		{name: "add", invoke: func(service *MutationService) error {
			return service.Add(ctx, mutationTestProfile("team-b"))
		}},
		{name: "update", invoke: func(service *MutationService) error {
			return service.Update(ctx, "team-a", ProfileChanges{Address: stringPointer("https://new.example.com")})
		}},
		{name: "remove", invoke: func(service *MutationService) error {
			return service.Remove(ctx, "team-a")
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			configurations := &mutationConfigStore{configuration: Configuration{Profiles: []Profile{mutationTestProfile("team-a")}}}
			credentials := newMutationCredentialStore()
			authenticator := &mutationAuthenticator{credentials: credentials}
			service := NewMutationService(configurations, credentials, authenticator)

			err := tt.invoke(service)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("operation error = %v, want context.Canceled", err)
			}
			if configurations.loadCalls != 0 || configurations.saveCalls != 0 || credentials.getCalls != 0 || credentials.setCalls != 0 || credentials.deleteCalls != 0 || len(authenticator.calls) != 0 {
				t.Fatal("canceled operation reached an external boundary")
			}
		})
	}
}

func TestMutationServiceRemoveRejectsMissingProfileWithoutChangingState(t *testing.T) {
	original := Configuration{Profiles: []Profile{mutationTestProfile("team-a")}, ActiveProfile: "team-a"}
	configurations := &mutationConfigStore{configuration: original}
	credentials := newMutationCredentialStore()
	credentials.credentials["team-a"] = "original-token"
	service := NewMutationService(configurations, credentials, &mutationAuthenticator{credentials: credentials})

	err := service.Remove(context.Background(), "missing")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("Remove() error = %v, want not found error", err)
	}
	if !reflect.DeepEqual(configurations.configuration, original) || configurations.saveCalls != 0 || credentials.getCalls != 0 || credentials.deleteCalls != 0 {
		t.Fatal("missing-profile Remove() changed state")
	}
}

func TestMutationServiceRemoveRestoresStateAfterConfigurationFailure(t *testing.T) {
	original := Configuration{Profiles: []Profile{mutationTestProfile("team-a")}, ActiveProfile: "team-a"}
	configurations := &mutationConfigStore{
		configuration: original,
		saveErrAt:     1,
		saveAfterErr:  true,
	}
	credentials := newMutationCredentialStore()
	credentials.credentials["team-a"] = "original-token"
	service := NewMutationService(configurations, credentials, &mutationAuthenticator{credentials: credentials})

	err := service.Remove(context.Background(), "team-a")
	if err == nil || !strings.Contains(err.Error(), "persist profile removal") {
		t.Fatalf("Remove() error = %v, want persistence error", err)
	}
	if !reflect.DeepEqual(configurations.configuration, original) {
		t.Fatalf("configuration after failed Remove() = %#v, want %#v", configurations.configuration, original)
	}
	if got := credentials.credentials["team-a"]; got != "original-token" {
		t.Fatalf("credential after failed Remove() = %q, want original token", got)
	}
	if credentials.deleteCalls != 0 {
		t.Fatalf("credential Delete() calls = %d, want 0", credentials.deleteCalls)
	}
}

func TestMutationServiceRemoveRestoresStateAfterCredentialDeletionFailure(t *testing.T) {
	for _, deleteAfter := range []bool{false, true} {
		name := "before deletion"
		if deleteAfter {
			name = "after deletion"
		}
		t.Run(name, func(t *testing.T) {
			original := Configuration{Profiles: []Profile{mutationTestProfile("team-a")}, ActiveProfile: "team-a"}
			configurations := &mutationConfigStore{configuration: original}
			credentials := newMutationCredentialStore()
			credentials.credentials["team-a"] = "original-token"
			credentials.deleteErr = errors.New("credential store unavailable")
			credentials.deleteAfter = deleteAfter
			service := NewMutationService(configurations, credentials, &mutationAuthenticator{credentials: credentials})

			err := service.Remove(context.Background(), "team-a")
			if err == nil || !strings.Contains(err.Error(), "delete credential") {
				t.Fatalf("Remove() error = %v, want credential deletion error", err)
			}
			if !reflect.DeepEqual(configurations.configuration, original) {
				t.Fatalf("configuration after failed Remove() = %#v, want %#v", configurations.configuration, original)
			}
			if got := credentials.credentials["team-a"]; got != "original-token" {
				t.Fatalf("credential after failed Remove() = %q, want original token", got)
			}
		})
	}
}

func TestMutationServiceRemoveStopsWhenOriginalCredentialCannotBeRead(t *testing.T) {
	original := Configuration{Profiles: []Profile{mutationTestProfile("team-a")}, ActiveProfile: "team-a"}
	configurations := &mutationConfigStore{configuration: original}
	credentials := newMutationCredentialStore()
	credentials.getErr = errors.New("credential store unavailable")
	service := NewMutationService(configurations, credentials, &mutationAuthenticator{credentials: credentials})

	err := service.Remove(context.Background(), "team-a")
	if err == nil || !strings.Contains(err.Error(), "load original credential") {
		t.Fatalf("Remove() error = %v, want credential load error", err)
	}
	if !reflect.DeepEqual(configurations.configuration, original) || configurations.saveCalls != 0 || credentials.deleteCalls != 0 {
		t.Fatal("failed credential snapshot changed removal state")
	}
}

func TestMutationServiceRemoveReportsIncompleteRollback(t *testing.T) {
	original := Configuration{Profiles: []Profile{mutationTestProfile("team-a")}, ActiveProfile: "team-a"}
	configurations := &mutationConfigStore{
		configuration: original,
		saveErrAt:     2,
	}
	credentials := newMutationCredentialStore()
	credentials.credentials["team-a"] = "original-token"
	credentials.deleteErr = errors.New("credential store unavailable")
	service := NewMutationService(configurations, credentials, &mutationAuthenticator{credentials: credentials})

	err := service.Remove(context.Background(), "team-a")
	if err == nil {
		t.Fatal("Remove() error = nil, want partial failure")
	}
	for _, want := range []string{"delete credential", "restore profile configuration"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Remove() error = %q, want %q", err, want)
		}
	}
	if configurations.configuration.ActiveProfile != "" {
		t.Fatalf("active profile after incomplete rollback = %q, want no fallback", configurations.configuration.ActiveProfile)
	}
}
