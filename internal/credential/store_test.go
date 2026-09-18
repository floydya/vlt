package credential

import (
	"context"
	"errors"
	"strings"
	"testing"
)

var errFakeNotFound = errors.New("fake keyring entry not found")

type fakeBackend struct {
	values    map[string]string
	getErr    error
	setErr    error
	deleteErr error
	calls     []backendCall
}

type backendCall struct {
	operation string
	service   string
	account   string
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{values: make(map[string]string)}
}

func (f *fakeBackend) Get(service, account string) (string, error) {
	f.calls = append(f.calls, backendCall{operation: "get", service: service, account: account})
	if f.getErr != nil {
		return "", f.getErr
	}
	value, found := f.values[service+"\x00"+account]
	if !found {
		return "", errFakeNotFound
	}
	return value, nil
}

func (f *fakeBackend) Set(service, account, value string) error {
	f.calls = append(f.calls, backendCall{operation: "set", service: service, account: account})
	if f.setErr != nil {
		return f.setErr
	}
	f.values[service+"\x00"+account] = value
	return nil
}

func (f *fakeBackend) Delete(service, account string) error {
	f.calls = append(f.calls, backendCall{operation: "delete", service: service, account: account})
	if f.deleteErr != nil {
		return f.deleteErr
	}
	key := service + "\x00" + account
	if _, found := f.values[key]; !found {
		return errFakeNotFound
	}
	delete(f.values, key)
	return nil
}

func TestStoreContractReplacesCredentialAtDeterministicNamespacedEntry(t *testing.T) {
	backend := newFakeBackend()
	store := newStore(backend, errFakeNotFound)
	ctx := context.Background()

	if err := store.Set(ctx, "team-a", "synthetic-token-one"); err != nil {
		t.Fatalf("first Set() error = %v", err)
	}
	if err := store.Set(ctx, "team-a", "synthetic-token-two"); err != nil {
		t.Fatalf("replacement Set() error = %v", err)
	}
	got, err := store.Get(ctx, "team-a")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != "synthetic-token-two" {
		t.Fatalf("Get() = %q, want replacement token", got)
	}
	if len(backend.values) != 1 {
		t.Fatalf("backend entries = %d, want exactly one replaced entry", len(backend.values))
	}
	for _, call := range backend.calls {
		if call.service != "vlt" || call.account != "team-a" {
			t.Errorf("backend call = %#v, want service vlt and account team-a", call)
		}
	}
}

func TestStoreContractDeletesCredentialAndReportsNotFound(t *testing.T) {
	backend := newFakeBackend()
	store := newStore(backend, errFakeNotFound)
	ctx := context.Background()

	if err := store.Set(ctx, "team-a", "synthetic-token"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if err := store.Delete(ctx, "team-a"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if _, err := store.Get(ctx, "team-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get() after Delete() error = %v, want ErrNotFound", err)
	}
	if err := store.Delete(ctx, "team-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Delete() error = %v, want ErrNotFound", err)
	}
}

func TestStoreContractReturnsActionableRedactedBackendFailures(t *testing.T) {
	const token = "CANARY-SYNTHETIC-TOKEN"
	tests := []struct {
		name      string
		configure func(*fakeBackend)
		invoke    func(*NativeStore) error
		wantOp    string
	}{
		{
			name:      "get",
			configure: func(backend *fakeBackend) { backend.getErr = errors.New("backend exposed " + token) },
			invoke: func(store *NativeStore) error {
				_, err := store.Get(context.Background(), "team-a")
				return err
			},
			wantOp: "load",
		},
		{
			name:      "set",
			configure: func(backend *fakeBackend) { backend.setErr = errors.New("backend exposed " + token) },
			invoke: func(store *NativeStore) error {
				return store.Set(context.Background(), "team-a", token)
			},
			wantOp: "store",
		},
		{
			name:      "delete",
			configure: func(backend *fakeBackend) { backend.deleteErr = errors.New("backend exposed " + token) },
			invoke: func(store *NativeStore) error {
				return store.Delete(context.Background(), "team-a")
			},
			wantOp: "delete",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := newFakeBackend()
			tt.configure(backend)
			err := tt.invoke(newStore(backend, errFakeNotFound))
			if !errors.Is(err, ErrUnavailable) {
				t.Fatalf("operation error = %v, want ErrUnavailable", err)
			}
			for _, want := range []string{tt.wantOp, "native credential store", "unavailable or locked"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("operation error = %q, want %q", err, want)
				}
			}
			if strings.Contains(err.Error(), token) {
				t.Fatalf("operation error exposed token: %q", err)
			}
		})
	}
}

func TestStoreContractRejectsInvalidInputsWithoutCallingBackend(t *testing.T) {
	tests := []struct {
		name   string
		invoke func(*NativeStore) error
	}{
		{
			name: "empty profile name",
			invoke: func(store *NativeStore) error {
				_, err := store.Get(context.Background(), "")
				return err
			},
		},
		{
			name: "numeric profile name",
			invoke: func(store *NativeStore) error {
				return store.Delete(context.Background(), "123")
			},
		},
		{
			name: "empty token",
			invoke: func(store *NativeStore) error {
				return store.Set(context.Background(), "team-a", "")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := newFakeBackend()
			err := tt.invoke(newStore(backend, errFakeNotFound))
			if err == nil {
				t.Fatal("operation error = nil, want input rejection")
			}
			if len(backend.calls) != 0 {
				t.Fatalf("backend calls = %#v, want none", backend.calls)
			}
		})
	}
}

func TestStoreContractHonorsCanceledContextWithoutCallingBackend(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name   string
		invoke func(*NativeStore) error
	}{
		{
			name: "get",
			invoke: func(store *NativeStore) error {
				_, err := store.Get(ctx, "team-a")
				return err
			},
		},
		{
			name:   "set",
			invoke: func(store *NativeStore) error { return store.Set(ctx, "team-a", "synthetic-token") },
		},
		{
			name:   "delete",
			invoke: func(store *NativeStore) error { return store.Delete(ctx, "team-a") },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := newFakeBackend()
			err := tt.invoke(newStore(backend, errFakeNotFound))
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("operation error = %v, want context.Canceled", err)
			}
			if len(backend.calls) != 0 {
				t.Fatalf("backend calls = %#v, want none", backend.calls)
			}
		})
	}
}
