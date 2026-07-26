package credentials

import (
	"errors"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"
)

type fakeBackend struct {
	values    map[string]string
	getErr    error
	setErr    error
	deleteErr error
}

func (f *fakeBackend) key(service, account string) string { return service + "\x00" + account }
func (f *fakeBackend) Get(service, account string) (string, error) {
	if f.getErr != nil {
		return "", f.getErr
	}
	value, ok := f.values[f.key(service, account)]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return value, nil
}
func (f *fakeBackend) Set(service, account, secret string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.values[f.key(service, account)] = secret
	return nil
}
func (f *fakeBackend) Delete(service, account string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	key := f.key(service, account)
	if _, ok := f.values[key]; !ok {
		return keyring.ErrNotFound
	}
	delete(f.values, key)
	return nil
}

func TestNormalizeOrigin(t *testing.T) {
	tests := map[string]string{
		"HTTPS://Example.COM:443/path?q=1#fragment": "https://example.com",
		"http://Example.COM:80/path":                "http://example.com",
		"https://Example.COM:8443/path":             "https://example.com:8443",
		"https://[2001:db8::1]:443/path":            "https://[2001:db8::1]",
	}
	for input, want := range tests {
		got, err := NormalizeOrigin(input)
		if err != nil || got != want {
			t.Errorf("NormalizeOrigin(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := NormalizeOrigin("not-a-url"); !IsKind(err, KindStore) {
		t.Fatalf("invalid origin error = %v", err)
	}
}

func TestKeyringStoreRoundTripAndOriginScoping(t *testing.T) {
	backend := &fakeBackend{values: make(map[string]string)}
	store := newKeyringStore(backend)
	first := Record{Username: "alice", Password: "secret"}
	second := Record{Username: "bob", Password: "other"}
	if err := store.Set("https://example.test/path", first); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("https://other.test", second); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("HTTPS://EXAMPLE.TEST:443/elsewhere")
	if err != nil || got.Version != 1 || got.Username != first.Username || got.Password != first.Password {
		t.Fatalf("Get() = %#v, %v", got, err)
	}
	if err := store.Delete("https://example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("https://example.test"); !IsKind(err, KindNotFound) {
		t.Fatalf("Get() after delete = %v", err)
	}
	if _, err := store.Get("https://other.test"); err != nil {
		t.Fatalf("other origin was affected: %v", err)
	}
}

func TestKeyringStoreClassifiesSafeErrors(t *testing.T) {
	backend := &fakeBackend{values: make(map[string]string)}
	store := newKeyringStore(backend)
	backend.getErr = errors.New("org.freedesktop.Secret.Error.IsLocked: sensitive native detail")
	if _, err := store.Get("https://example.test"); !IsKind(err, KindLocked) || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("locked error = %v", err)
	}
	backend.getErr = errors.New("cannot autolaunch D-Bus without X11")
	if _, err := store.Get("https://example.test"); !IsKind(err, KindUnavailable) {
		t.Fatalf("unavailable error = %v", err)
	}
	backend.getErr = nil
	backend.values[backend.key(serviceName, "https://example.test")] = `{"version":99,"username":"alice","password":"do-not-leak"}`
	if _, err := store.Get("https://example.test"); !IsKind(err, KindCorrupt) || strings.Contains(err.Error(), "do-not-leak") {
		t.Fatalf("corrupt error = %v", err)
	}
}
