package credentials

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestFileStoreRoundTripPermissionsAndOriginScoping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "credentials.json")
	store := NewFileStore(path)
	first := Record{Username: "alice", Password: "secret"}
	second := Record{Username: "bob", Password: "other"}

	if err := store.Set("https://example.test/path", first); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("https://other.test", second); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get("HTTPS://EXAMPLE.TEST:443/elsewhere")
	if err != nil || got.Username != first.Username || got.Password != first.Password {
		t.Fatalf("Get() = %#v, %v", got, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("credentials mode = %v; want 0600", info.Mode().Perm())
		}
		directory, err := os.Stat(filepath.Dir(path))
		if err != nil {
			t.Fatal(err)
		}
		if directory.Mode().Perm() != 0o700 {
			t.Fatalf("credentials directory mode = %v; want 0700", directory.Mode().Perm())
		}
	}

	if err := store.Delete("https://example.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("https://example.test"); !IsKind(err, KindNotFound) {
		t.Fatalf("Get() after scoped delete = %v", err)
	}
	if _, err := store.Get("https://other.test"); err != nil {
		t.Fatalf("other origin was affected: %v", err)
	}
	if err := store.Delete("https://other.test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("empty credentials file still exists: %v", err)
	}
}

func TestFileStoreRejectsLoosePermissionsAndCorruptData(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	path := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"origins":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewFileStore(path)
	if _, err := store.Get("https://example.test"); !IsKind(err, KindStore) {
		t.Fatalf("loose permissions error = %v", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`not-json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("https://example.test"); !IsKind(err, KindCorrupt) {
		t.Fatalf("corrupt file error = %v", err)
	}
}
