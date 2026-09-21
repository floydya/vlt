package favorite

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
)

func testFavorite(profileName, operation, path, note string) Favorite {
	return Favorite{Profile: profileName, Operation: operation, Path: path, Note: note}
}

func testFavoritePath(t *testing.T) string {
	t.Helper()
	directory := filepath.Join(t.TempDir(), "vlt")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("Mkdir(test favorite directory) error = %v", err)
	}
	return filepath.Join(directory, "favorites.json")
}

func TestStoreRoundTripPreservesAcceptedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vlt", "favorites.json")
	store := NewStore(path)
	want := Configuration{Favorites: []Favorite{
		testFavorite("team-a", OperationRead, " secret/data/platform ", " daily credentials "),
		testFavorite("Team_B2", OperationKVGet, "secret/data/reporting", ""),
	}}
	want.Favorites[1].RunCount = 9
	for index := range want.Favorites {
		want.Favorites[index].ID = legacyID(want.Favorites[index], 0)
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
		t.Fatalf("saved favorites are not JSON: %v", err)
	}
	assertFavoriteJSONKeys(t, document, map[string]bool{"version": true, "favorites": true})
	if gotVersion := document["version"]; gotVersion != float64(2) {
		t.Fatalf("JSON version = %#v, want 2", gotVersion)
	}
	entries, ok := document["favorites"].([]any)
	if !ok || len(entries) != 2 {
		t.Fatalf("JSON favorites = %#v, want two favorites", document["favorites"])
	}
	for index, value := range entries {
		entry, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("JSON favorite = %#v, want object", value)
		}
		wantKeys := map[string]bool{
			"id": true, "profile": true, "operation": true, "path": true, "note": true,
		}
		if index == 1 {
			wantKeys["run_count"] = true
			if entry["run_count"] != float64(9) {
				t.Fatalf("saved run count = %#v, want 9", entry["run_count"])
			}
		}
		assertFavoriteJSONKeys(t, entry, wantKeys)
	}
}

func TestStoreLoadsLegacyFavoriteWithZeroRunCount(t *testing.T) {
	path := testFavoritePath(t)
	contents := []byte(`{"version":1,"favorites":[{"profile":"team-a","operation":"read","path":"secret/a","note":"old"}]}`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	got, err := NewStore(path).Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.Favorites) != 1 || got.Favorites[0].RunCount != 0 {
		t.Fatalf("Load() = %#v, want one favorite with zero runs", got)
	}
}

func assertFavoriteJSONKeys(t *testing.T, got map[string]any, want map[string]bool) {
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
	store := NewStore(filepath.Join(t.TempDir(), "missing", "favorites.json"))
	got, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(got.Favorites) != 0 {
		t.Fatalf("Load() = %#v, want empty configuration", got)
	}
}

func TestStoreRejectsMalformedInvalidOrDuplicateConfiguration(t *testing.T) {
	valid := `{"profile":"team-a","operation":"read","path":"secret/a","note":"first"}`
	duplicateWithDifferentNote := `{"profile":"team-a","operation":"read","path":"secret/a","note":"second"}`
	tests := []struct {
		name     string
		contents string
		wantErr  string
	}{
		{name: "empty document", contents: "", wantErr: "decode"},
		{name: "malformed JSON", contents: `{"version":1`, wantErr: "decode"},
		{name: "trailing JSON", contents: `{"version":1,"favorites":[]} {}`, wantErr: "single JSON"},
		{name: "missing version", contents: `{"favorites":[]}`, wantErr: "version"},
		{name: "unsupported version", contents: `{"version":3,"favorites":[]}`, wantErr: "version"},
		{name: "unknown top level field", contents: `{"version":1,"favorites":[],"token":"synthetic"}`, wantErr: "unknown field"},
		{name: "unknown favorite field", contents: `{"version":1,"favorites":[{"profile":"team-a","operation":"read","path":"secret/a","note":"","value":"synthetic"}]}`, wantErr: "unknown field"},
		{name: "invalid field type", contents: `{"version":1,"favorites":7}`, wantErr: "decode"},
		{name: "invalid profile", contents: `{"version":1,"favorites":[{"profile":"bad name","operation":"read","path":"secret/a","note":""}]}`, wantErr: "profile"},
		{name: "invalid operation", contents: `{"version":1,"favorites":[{"profile":"team-a","operation":"write","path":"secret/a","note":""}]}`, wantErr: "operation"},
		{name: "blank path", contents: `{"version":1,"favorites":[{"profile":"team-a","operation":"read","path":" ","note":""}]}`, wantErr: "path"},
		{name: "negative count", contents: `{"version":1,"favorites":[{"profile":"team-a","operation":"read","path":"secret/a","run_count":-1}]}`, wantErr: "run count"},
		{name: "fractional count", contents: `{"version":1,"favorites":[{"profile":"team-a","operation":"read","path":"secret/a","run_count":1.5}]}`, wantErr: "decode"},
		{name: "string count", contents: `{"version":1,"favorites":[{"profile":"team-a","operation":"read","path":"secret/a","run_count":"2"}]}`, wantErr: "decode"},
		{name: "null count", contents: `{"version":1,"favorites":[{"profile":"team-a","operation":"read","path":"secret/a","run_count":null}]}`, wantErr: "decode"},
		{name: "overflowing count", contents: `{"version":1,"favorites":[{"profile":"team-a","operation":"read","path":"secret/a","run_count":9223372036854775808}]}`, wantErr: "decode"},
		{name: "duplicate tuple with different note", contents: `{"version":1,"favorites":[` + valid + `,` + duplicateWithDifferentNote + `]}`, wantErr: "duplicate"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := testFavoritePath(t)
			if err := os.WriteFile(path, []byte(tt.contents), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			_, err := NewStore(path).Load(context.Background())
			if err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.wantErr)) {
				t.Fatalf("Load() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestStoreRejectsUnsafePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	path := testFavoritePath(t)
	contents := []byte(`{"version":1,"favorites":[]}`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Chmod(path, 0o640); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	if _, err := NewStore(path).Load(context.Background()); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("Load() error = %v, want unsafe permissions rejection", err)
	}
}

func TestStoreRejectsUnsafeApplicationDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses a platform-specific path policy")
	}
	directory := filepath.Join(t.TempDir(), "vlt")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	path := filepath.Join(directory, "favorites.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"favorites":[]}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := os.Chmod(directory, 0o750); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}

	if _, err := NewStore(path).Load(context.Background()); err == nil || !strings.Contains(err.Error(), "directory permissions") {
		t.Fatalf("Load() error = %v, want unsafe directory rejection", err)
	}
	if err := NewStore(path).Save(context.Background(), Configuration{}); err == nil || !strings.Contains(err.Error(), "directory permissions") {
		t.Fatalf("Save() error = %v, want unsafe directory rejection", err)
	}
}

func TestStoreRejectsUnsafeExistingStateWithoutReplacingIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows uses a platform-specific path policy")
	}
	original := []byte(`{"version":1,"favorites":[]}`)
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
			path := filepath.Join(directory, "favorites.json")
			preservedPath := tt.setup(t, path, root)
			before, err := os.ReadFile(preservedPath)
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}

			err = NewStore(path).Save(context.Background(), Configuration{Favorites: []Favorite{testFavorite("team-a", OperationRead, "secret/a", "")}})
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
	if err := os.WriteFile(filepath.Join(realDirectory, "favorites.json"), []byte(`{"version":1,"favorites":[]}`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	linkedDirectory := filepath.Join(root, "linked")
	if err := os.Symlink(realDirectory, linkedDirectory); err != nil {
		t.Fatalf("Symlink(directory) error = %v", err)
	}
	if _, err := NewStore(filepath.Join(linkedDirectory, "favorites.json")).Load(context.Background()); err == nil || !strings.Contains(err.Error(), "symbolic link") {
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

func TestStoreRejectsDuplicateWithoutChangingExistingConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vlt", "favorites.json")
	store := NewStore(path)
	original := Configuration{Favorites: []Favorite{testFavorite("team-a", OperationRead, "secret/a", "original")}}
	if err := store.Save(context.Background(), original); err != nil {
		t.Fatalf("initial Save() error = %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	duplicate := Configuration{Favorites: []Favorite{
		testFavorite("team-a", OperationRead, "secret/a", "first"),
		testFavorite("team-a", OperationRead, "secret/a", "second"),
	}}
	err = store.Save(context.Background(), duplicate)
	if !errors.Is(err, ErrDuplicateFavorite) {
		t.Fatalf("duplicate Save() error = %v, want ErrDuplicateFavorite", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() after failed save error = %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("configuration changed after duplicate rejection\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestStoreAtomicWriteFailurePreservesPriorConfiguration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vlt", "favorites.json")
	store := NewStore(path)
	original := Configuration{Favorites: []Favorite{testFavorite("team-a", OperationRead, "secret/a", "original")}}
	if err := store.Save(context.Background(), original); err != nil {
		t.Fatalf("initial Save() error = %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	store.renameFile = func(string, string) error { return errors.New("injected rename failure") }
	replacement := Configuration{Favorites: []Favorite{testFavorite("team-b", OperationKVGet, "secret/b", "replacement")}}
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
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".favorites.json-*.tmp"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files left after failed save: %v", matches)
	}
}

func TestStoreCreatesPrivateDirectoryAndFile(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "new", "vlt")
	path := filepath.Join(directory, "favorites.json")
	store := NewStore(path)
	configuration := Configuration{Favorites: []Favorite{testFavorite("team-a", OperationRead, "secret/a", "")}}
	if err := store.Save(context.Background(), configuration); err != nil {
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
	path := filepath.Join(t.TempDir(), "vlt", "favorites.json")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := NewStore(path).Save(ctx, Configuration{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Save() error = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stat() error = %v, want file not to exist", err)
	}
}
