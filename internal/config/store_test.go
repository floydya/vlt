package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"vlt/internal/profile"
)

func testProfile(name string) profile.Profile {
	return profile.Profile{
		Name:      name,
		Address:   "https://vault.example.com/" + name,
		Username:  "user-" + name,
		AuthPath:  "oidc",
		Namespace: "engineering/" + name,
	}
}

func testConfigPath(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "vlt")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("Mkdir(test config directory) error = %v", err)
	}
	return filepath.Join(directory, "profiles.json")
}

func TestStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vlt", "profiles.json")
	store := NewStore(path)
	want := Configuration{
		Profiles: []profile.Profile{
			testProfile("team-a"),
			func() profile.Profile {
				candidate := testProfile("Team_B2")
				candidate.Address = "http://127.0.0.1:8200"
				candidate.AllowInsecure = true
				candidate.Color = "#a1B2c3"
				return candidate
			}(),
		},
		ActiveProfile: "Team_B2",
	}

	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(contents, &document); err != nil {
		t.Fatalf("saved configuration is not JSON: %v", err)
	}
	if gotVersion := document["version"]; gotVersion != float64(1) {
		t.Errorf("JSON version = %#v, want 1", gotVersion)
	}
	if gotActive := document["active_profile"]; gotActive != "Team_B2" {
		t.Errorf("JSON active_profile = %#v, want Team_B2", gotActive)
	}
	assertJSONKeys(t, document, map[string]bool{
		"version": true, "profiles": true, "active_profile": true,
	})
	profiles, ok := document["profiles"].([]any)
	if !ok || len(profiles) != 2 {
		t.Fatalf("JSON profiles = %#v, want two profiles", document["profiles"])
	}
	for index, value := range profiles {
		entry, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("JSON profile = %#v, want object", value)
		}
		wantKeys := map[string]bool{
			"name": true, "address": true, "username": true, "auth_path": true, "namespace": true,
			"allow_insecure": true,
		}
		if index == 1 {
			wantKeys["color"] = true
			if entry["color"] != "#a1B2c3" {
				t.Fatalf("saved color = %#v, want #a1B2c3", entry["color"])
			}
		}
		assertJSONKeys(t, entry, wantKeys)
	}
	lower := strings.ToLower(string(contents))
	for _, forbidden := range []string{"token", "keyring", "credential", "secret"} {
		if strings.Contains(lower, forbidden) {
			t.Errorf("serialized configuration contains forbidden term %q: %s", forbidden, contents)
		}
	}
}

func TestStoreLoadsLegacyHTTPSProfileWithoutInsecureOptIn(t *testing.T) {
	path := testConfigPath(t)
	contents := `{"version":1,"profiles":[{"name":"team-a","address":"https://vault.example.com","username":"user","auth_path":"oidc","namespace":""}],"active_profile":"team-a"}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	configuration, err := NewStore(path).Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(configuration.Profiles) != 1 {
		t.Fatalf("profile count = %d, want 1", len(configuration.Profiles))
	}
	if configuration.Profiles[0].AllowInsecure {
		t.Fatal("legacy profile AllowInsecure = true, want secure default")
	}
	if configuration.Profiles[0].Color != "" {
		t.Fatalf("legacy profile color = %q, want empty", configuration.Profiles[0].Color)
	}
}

func TestStoreRejectsInvalidColorWithoutReplacingConfiguration(t *testing.T) {
	path := testConfigPath(t)
	store := NewStore(path)
	original := Configuration{Profiles: []profile.Profile{testProfile("team-a")}}
	if err := store.Save(context.Background(), original); err != nil {
		t.Fatalf("initial Save() error = %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	invalid := Configuration{Profiles: []profile.Profile{testProfile("team-a")}}
	invalid.Profiles[0].Color = "#GGGGGG"
	if err := store.Save(context.Background(), invalid); err == nil || !strings.Contains(err.Error(), "color") {
		t.Fatalf("Save(invalid color) error = %v, want color validation", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() after invalid Save error = %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("configuration changed after invalid color")
	}
}

func TestStoreRejectsLegacyHTTPProfileWithoutInsecureOptIn(t *testing.T) {
	path := testConfigPath(t)
	contents := `{"version":1,"profiles":[{"name":"team-a","address":"http://127.0.0.1:8200","username":"user","auth_path":"oidc","namespace":""}],"active_profile":"team-a"}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	_, err := NewStore(path).Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "allow-insecure") {
		t.Fatalf("Load() error = %v, want insecure transport diagnostic", err)
	}
}

func assertJSONKeys(t *testing.T, got map[string]any, want map[string]bool) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("JSON keys = %v, want exactly %v", reflect.ValueOf(got).MapKeys(), reflect.ValueOf(want).MapKeys())
	}
	for key := range got {
		if !want[key] {
			t.Errorf("unexpected JSON key %q", key)
		}
	}
}

func TestStoreLoadMissingFileReturnsEmptyConfiguration(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "missing", "profiles.json"))
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.ActiveProfile != "" || len(got.Profiles) != 0 {
		t.Fatalf("Load() = %#v, want empty configuration", got)
	}
}

func TestStoreRejectsMalformedOrUnsafeConfiguration(t *testing.T) {
	validProfile := `{"name":"team-a","address":"https://vault.example.com","username":"user","auth_path":"oidc"}`
	tests := []struct {
		name     string
		contents string
		wantErr  string
	}{
		{name: "empty document", contents: "", wantErr: "decode"},
		{name: "malformed JSON", contents: `{"version":1`, wantErr: "decode"},
		{name: "trailing JSON", contents: `{"version":1,"profiles":[]} {}`, wantErr: "single JSON"},
		{name: "missing version", contents: `{"profiles":[]}`, wantErr: "version"},
		{name: "unsupported version", contents: `{"version":2,"profiles":[]}`, wantErr: "version"},
		{name: "unknown top level field", contents: `{"version":1,"profiles":[],"token":"synthetic-value"}`, wantErr: "unknown field"},
		{name: "unknown profile field", contents: `{"version":1,"profiles":[{"name":"team-a","address":"https://vault.example.com","username":"user","auth_path":"oidc","keyring_id":"entry"}]}`, wantErr: "unknown field"},
		{name: "invalid field type", contents: `{"version":1,"profiles":[],"active_profile":7}`, wantErr: "decode"},
		{name: "invalid profile", contents: `{"version":1,"profiles":[{"name":"bad name","address":"https://vault.example.com","username":"user","auth_path":"oidc"}]}`, wantErr: "profile"},
		{name: "invalid color", contents: `{"version":1,"profiles":[{"name":"team-a","address":"https://vault.example.com","username":"user","auth_path":"oidc","color":"red"}]}`, wantErr: "color"},
		{name: "duplicate profile name", contents: `{"version":1,"profiles":[` + validProfile + `,` + validProfile + `]}`, wantErr: "duplicate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := testConfigPath(t)
			if err := os.WriteFile(path, []byte(tt.contents), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			_, err := NewStore(path).Load(context.Background())
			if err == nil {
				t.Fatal("Load() error = nil, want error")
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantErr)) {
				t.Fatalf("Load() error = %q, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestStoreLoadErrorsDoNotExposeUntrustedValues(t *testing.T) {
	const canary = "CANARYLEAK"
	tests := []struct {
		name     string
		contents string
	}{
		{
			name:     "unknown schema field",
			contents: `{"version":1,"profiles":[],"` + canary + `":"value"}`,
		},
		{
			name: "duplicate profile name",
			contents: `{"version":1,"profiles":[` +
				`{"name":"` + canary + `","address":"https://vault.example.com","username":"user","auth_path":"oidc"},` +
				`{"name":"` + canary + `","address":"https://vault.example.com","username":"user","auth_path":"oidc"}]}`,
		},
		{
			name:     "missing active profile",
			contents: `{"version":1,"profiles":[],"active_profile":"` + canary + `"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := testConfigPath(t)
			if err := os.WriteFile(path, []byte(tt.contents), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			_, err := NewStore(path).Load(context.Background())
			if err == nil {
				t.Fatal("Load() error = nil, want error")
			}
			if strings.Contains(err.Error(), canary) {
				t.Fatalf("Load() error exposed configuration content: %q", err)
			}
		})
	}
}

func TestStoreRejectsUnsafeApplicationDirectoryAndFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses a platform-specific path policy")
	}
	valid := []byte(`{"version":1,"profiles":[],"active_profile":""}`)
	tests := []struct {
		name          string
		directoryMode os.FileMode
		fileMode      os.FileMode
		want          string
	}{
		{name: "group-accessible directory", directoryMode: 0o750, fileMode: 0o600, want: "directory permissions"},
		{name: "group-readable file", directoryMode: 0o700, fileMode: 0o640, want: "file permissions"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "vlt")
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatalf("Mkdir() error = %v", err)
			}
			path := filepath.Join(directory, "profiles.json")
			if err := os.WriteFile(path, valid, 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			if err := os.Chmod(path, tt.fileMode); err != nil {
				t.Fatalf("Chmod(file) error = %v", err)
			}
			if err := os.Chmod(directory, tt.directoryMode); err != nil {
				t.Fatalf("Chmod(directory) error = %v", err)
			}

			_, err := NewStore(path).Load(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want %q", err, tt.want)
			}
			if tt.directoryMode != 0o700 {
				err = NewStore(path).Save(context.Background(), Configuration{})
				if err == nil || !strings.Contains(err.Error(), tt.want) {
					t.Fatalf("Save() error = %v, want %q", err, tt.want)
				}
			}
		})
	}
}

func TestStoreRejectsUnsafeExistingStateWithoutReplacingIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses a platform-specific path policy")
	}
	original := []byte(`{"version":1,"profiles":[],"active_profile":""}`)
	tests := []struct {
		name  string
		setup func(*testing.T, string, string) string
		want  string
	}{
		{
			name: "permissive file",
			setup: func(t *testing.T, path, _ string) string {
				t.Helper()
				if err := os.WriteFile(path, original, 0o640); err != nil {
					t.Fatalf("WriteFile() error = %v", err)
				}
				return path
			},
			want: "file permissions",
		},
		{
			name: "file symlink",
			setup: func(t *testing.T, path, root string) string {
				t.Helper()
				target := filepath.Join(root, "outside.json")
				if err := os.WriteFile(target, original, 0o600); err != nil {
					t.Fatalf("WriteFile(target) error = %v", err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatalf("Symlink() error = %v", err)
				}
				return target
			},
			want: "symbolic link",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, "vlt")
			if err := os.Mkdir(directory, 0o700); err != nil {
				t.Fatalf("Mkdir() error = %v", err)
			}
			path := filepath.Join(directory, "profiles.json")
			preservedPath := tt.setup(t, path, root)
			before, err := os.ReadFile(preservedPath)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}

			err = NewStore(path).Save(context.Background(), Configuration{Profiles: []profile.Profile{testProfile("team-a")}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Save() error = %v, want %q", err, tt.want)
			}
			after, readErr := os.ReadFile(preservedPath)
			if readErr != nil {
				t.Fatalf("ReadFile() after Save error = %v", readErr)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("unsafe existing state changed\nbefore: %s\nafter: %s", before, after)
			}
		})
	}
}

func TestStoreRejectsSymlinkedApplicationDirectoryAndNonRegularFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink creation requires additional privileges")
	}
	root := t.TempDir()
	realDirectory := filepath.Join(root, "real")
	if err := os.Mkdir(realDirectory, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	valid := []byte(`{"version":1,"profiles":[],"active_profile":""}`)
	if err := os.WriteFile(filepath.Join(realDirectory, "profiles.json"), valid, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	linkedDirectory := filepath.Join(root, "linked")
	if err := os.Symlink(realDirectory, linkedDirectory); err != nil {
		t.Fatalf("Symlink(directory) error = %v", err)
	}
	if _, err := NewStore(filepath.Join(linkedDirectory, "profiles.json")).Load(context.Background()); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Load() through directory symlink error = %v, want symbolic link rejection", err)
	}

	directoryPath := filepath.Join(realDirectory, "metadata-directory")
	if err := os.Mkdir(directoryPath, 0o700); err != nil {
		t.Fatalf("Mkdir(metadata path) error = %v", err)
	}
	if _, err := NewStore(directoryPath).Load(context.Background()); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("Load() directory error = %v, want non-regular rejection", err)
	}
}

func TestStoreRejectsDuplicateNamesWithoutChangingExistingConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vlt", "profiles.json")
	store := NewStore(path)
	original := Configuration{Profiles: []profile.Profile{testProfile("team-a")}, ActiveProfile: "team-a"}
	if err := store.Save(context.Background(), original); err != nil {
		t.Fatalf("initial Save() error = %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	duplicate := Configuration{Profiles: []profile.Profile{testProfile("team-b"), testProfile("team-b")}}
	err = store.Save(context.Background(), duplicate)
	if !errors.Is(err, ErrDuplicateProfile) {
		t.Fatalf("duplicate Save() error = %v, want ErrDuplicateProfile", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() after failed save error = %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("configuration changed after duplicate rejection\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestStoreRejectsNumericOnlyNameWithoutChangingExistingConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vlt", "profiles.json")
	store := NewStore(path)
	original := Configuration{Profiles: []profile.Profile{testProfile("team-a")}, ActiveProfile: "team-a"}
	if err := store.Save(context.Background(), original); err != nil {
		t.Fatalf("initial Save() error = %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	err = store.Save(context.Background(), Configuration{Profiles: []profile.Profile{testProfile("123")}})
	if err == nil || !strings.Contains(err.Error(), "digits") {
		t.Fatalf("numeric-name Save() error = %v, want digits validation error", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() after failed save error = %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("configuration changed after numeric name rejection\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestStoreAtomicWriteFailurePreservesPriorConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vlt", "profiles.json")
	store := NewStore(path)
	original := Configuration{Profiles: []profile.Profile{testProfile("team-a")}, ActiveProfile: "team-a"}
	if err := store.Save(context.Background(), original); err != nil {
		t.Fatalf("initial Save() error = %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	store.renameFile = func(string, string) error { return errors.New("injected rename failure") }
	replacement := Configuration{Profiles: []profile.Profile{testProfile("team-b")}, ActiveProfile: "team-b"}
	if err := store.Save(context.Background(), replacement); err == nil || !strings.Contains(err.Error(), "rename") {
		t.Fatalf("Save() error = %v, want injected rename failure", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() after failed save error = %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("configuration changed after failed atomic write\nbefore: %s\nafter:  %s", before, after)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".profiles.json-*.tmp"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files left after failed save: %v", matches)
	}
}

func TestStoreReportsDirectorySyncFailureAfterReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vlt", "profiles.json")
	store := NewStore(path)
	original := Configuration{Profiles: []profile.Profile{testProfile("team-a")}, ActiveProfile: "team-a"}
	if err := store.Save(context.Background(), original); err != nil {
		t.Fatalf("initial Save() error = %v", err)
	}

	const injected = "injected directory sync failure"
	store.syncDirectory = func(string) error { return errors.New(injected) }
	replacement := Configuration{Profiles: []profile.Profile{testProfile("team-b")}, ActiveProfile: "team-b"}
	err := store.Save(context.Background(), replacement)
	if err == nil {
		t.Fatal("Save() error = nil, want directory sync failure")
	}
	for _, want := range []string{"replaced", "directory", injected} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Save() error = %q, want %q", err, want)
		}
	}

	got, loadErr := NewStore(path).Load(context.Background())
	if loadErr != nil {
		t.Fatalf("Load() after directory sync failure error = %v", loadErr)
	}
	if !reflect.DeepEqual(got, replacement) {
		t.Fatalf("Load() after directory sync failure = %#v, want replacement %#v", got, replacement)
	}
}

func TestStoreCreatesPrivateDirectoryAndFile(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "new", "vlt")
	path := filepath.Join(directory, "profiles.json")
	store := NewStore(path)
	if err := store.Save(context.Background(), Configuration{Profiles: []profile.Profile{testProfile("team-a")}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	if runtime.GOOS != "windows" {
		directoryInfo, err := os.Stat(directory)
		if err != nil {
			t.Fatalf("Stat(directory) error = %v", err)
		}
		if got := directoryInfo.Mode().Perm(); got != 0o700 {
			t.Errorf("directory permissions = %04o, want 0700", got)
		}
		fileInfo, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat(file) error = %v", err)
		}
		if got := fileInfo.Mode().Perm(); got != 0o600 {
			t.Errorf("file permissions = %04o, want 0600", got)
		}
	}
}

func TestStoreHonorsCanceledContextWithoutWriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vlt", "profiles.json")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := NewStore(path).Save(ctx, Configuration{Profiles: []profile.Profile{testProfile("team-a")}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Save() error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat() error = %v, want file not to exist", err)
	}
}

func TestStoreSetActiveProfilePersistsResolvedSelection(t *testing.T) {
	tests := []struct {
		name     string
		profiles []profile.Profile
		selector string
		want     string
	}{
		{
			name:     "one profile by name",
			profiles: []profile.Profile{testProfile("team-a")},
			selector: "team-a",
			want:     "team-a",
		},
		{
			name:     "one profile by number",
			profiles: []profile.Profile{testProfile("team-a")},
			selector: "1",
			want:     "team-a",
		},
		{
			name: "multiple profiles by sorted number",
			profiles: []profile.Profile{
				testProfile("zulu"),
				testProfile("alpha"),
				testProfile("beta"),
			},
			selector: "2",
			want:     "beta",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "vlt", "profiles.json")
			store := NewStore(path)
			if err := store.Save(context.Background(), Configuration{Profiles: tt.profiles}); err != nil {
				t.Fatalf("initial Save() error = %v", err)
			}

			if err := store.SetActiveProfile(context.Background(), tt.selector); err != nil {
				t.Fatalf("SetActiveProfile(%q) error = %v", tt.selector, err)
			}
			got, err := NewStore(path).Load(context.Background())
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if got.ActiveProfile != tt.want {
				t.Fatalf("ActiveProfile = %q, want %q", got.ActiveProfile, tt.want)
			}
		})
	}
}

func TestStoreSetActiveProfileFailureLeavesSelectionUnchanged(t *testing.T) {
	selectors := []string{
		"TEAM-A", "missing", "0", "3", "-1", "+1", "01", " 1", "1 ", "１", "1.0",
		"999999999999999999999999999999999999999999999999999999999999999999",
	}

	for _, selector := range selectors {
		t.Run(selector, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "vlt", "profiles.json")
			store := NewStore(path)
			original := Configuration{
				Profiles:      []profile.Profile{testProfile("team-a"), testProfile("team-b")},
				ActiveProfile: "team-a",
			}
			if err := store.Save(context.Background(), original); err != nil {
				t.Fatalf("initial Save() error = %v", err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}

			if err := store.SetActiveProfile(context.Background(), selector); err == nil {
				t.Fatalf("SetActiveProfile(%q) error = nil, want rejection", selector)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile() after failed selection error = %v", err)
			}
			if !bytes.Equal(after, before) {
				t.Fatalf("configuration changed after SetActiveProfile(%q) failure\nbefore: %s\nafter:  %s", selector, before, after)
			}
			got, err := NewStore(path).Load(context.Background())
			if err != nil {
				t.Fatalf("Load() after failed selection error = %v", err)
			}
			if got.ActiveProfile != original.ActiveProfile {
				t.Fatalf("ActiveProfile after SetActiveProfile(%q) = %q, want %q", selector, got.ActiveProfile, original.ActiveProfile)
			}
		})
	}
}

func TestStoreSetActiveProfileOnEmptyConfigurationDoesNotCreateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vlt", "profiles.json")

	if err := NewStore(path).SetActiveProfile(context.Background(), "1"); err == nil {
		t.Fatal("SetActiveProfile() error = nil, want rejection")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat() error = %v, want configuration not to exist", err)
	}
}

func TestStoreClearActiveProfileLeavesProfilesWithoutFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vlt", "profiles.json")
	store := NewStore(path)
	profiles := []profile.Profile{testProfile("alpha"), testProfile("beta"), testProfile("gamma")}
	if err := store.Save(context.Background(), Configuration{Profiles: profiles, ActiveProfile: "beta"}); err != nil {
		t.Fatalf("initial Save() error = %v", err)
	}

	if err := store.ClearActiveProfile(context.Background()); err != nil {
		t.Fatalf("ClearActiveProfile() error = %v", err)
	}
	got, err := NewStore(path).Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.ActiveProfile != "" {
		t.Fatalf("ActiveProfile = %q, want no active profile", got.ActiveProfile)
	}
	if !reflect.DeepEqual(got.Profiles, profiles) {
		t.Fatalf("Profiles = %#v, want unchanged %#v", got.Profiles, profiles)
	}
}

func TestStoreSaveRemovingActiveProfileDoesNotChooseFallback(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vlt", "profiles.json")
	store := NewStore(path)
	if err := store.Save(context.Background(), Configuration{
		Profiles:      []profile.Profile{testProfile("alpha"), testProfile("beta"), testProfile("gamma")},
		ActiveProfile: "beta",
	}); err != nil {
		t.Fatalf("initial Save() error = %v", err)
	}

	remaining := []profile.Profile{testProfile("alpha"), testProfile("gamma")}
	if err := store.Save(context.Background(), Configuration{Profiles: remaining}); err != nil {
		t.Fatalf("Save() after removing active profile error = %v", err)
	}
	got, err := NewStore(path).Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.ActiveProfile != "" {
		t.Fatalf("ActiveProfile = %q, want no implicit fallback", got.ActiveProfile)
	}
	if !reflect.DeepEqual(got.Profiles, remaining) {
		t.Fatalf("Profiles = %#v, want %#v", got.Profiles, remaining)
	}
}
