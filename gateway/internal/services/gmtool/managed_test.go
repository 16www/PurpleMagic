package gmtool

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func TestManagedCredentialDigest(t *testing.T) {
	key := strings.Repeat("secret-fixture-", 4)
	digest := sha256.Sum256([]byte(key))
	hash := hex.EncodeToString(digest[:])
	s := &Server{cfg: Config{Password: hash, LoginKeySHA256: hash, PVFPath: "private-path"}, sessions: map[string]time.Time{}, failures: map[string][]time.Time{}}
	for _, input := range []string{hash, key} {
		r := httptest.NewRequest("POST", "https://example.invalid/api/auth/login", strings.NewReader(`{"password":"`+input+`"}`))
		w := httptest.NewRecorder()
		s.ManagementHandler("").ServeHTTP(w, r)
		cookies := w.Result().Cookies()
		if input == hash {
			if len(cookies) != 0 {
				t.Fatal("digest accepted as credential")
			}
		} else if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly {
			t.Fatal("missing secure session")
		}
	}
	w := httptest.NewRecorder()
	s.ManagementHandler("").ServeHTTP(w, httptest.NewRequest("GET", "/api/status", nil))
	if strings.Contains(w.Body.String(), "private-path") {
		t.Fatal("private path in status")
	}
}

func TestManagedMySQLPoolOwnershipAndMissingIndex(t *testing.T) {
	fixture, _, open := currencyFixture(t, 1)
	path := filepath.Join(t.TempDir(), "missing.json")
	calls := 0
	s, err := NewManaged(context.Background(), Config{Password: "fixture", Table: "web_accounts", ItemIndexCachePath: path}, Deps{RegistrationDB: fixture.db, Repos: func() (repository.Group, bool) { return open(), true }, PVF: func() (dnfpvf.Source, error) { calls++; return nil, errors.New("offline importer required") }})
	if err != nil {
		t.Fatal(err)
	}
	if s.items.isReady() {
		t.Fatal("missing index reported ready")
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("startup wrote an index", err)
	}
	if calls > 1 {
		t.Fatal("startup rebuilt PVF", calls)
	}
	if err = s.Stop(); err != nil {
		t.Fatal(err)
	}
	if err = fixture.db.Ping(); err != nil {
		t.Fatal("caller-owned pool closed", err)
	}
}
