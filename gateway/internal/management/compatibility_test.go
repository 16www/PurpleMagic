package management

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLegacyRegistrationAndRestartConfiguration(t *testing.T) {
	cfg := Config{Listen: "127.0.0.1:8768", Password: strings.Repeat("p", 32), RegistrationDSN: "u:p@tcp(127.0.0.1:3306)/registration?parseTime=true", RepositoryDSN: "u:p@tcp(127.0.0.1:3306)/game?parseTime=true", AccountTable: "accounts", Databases: []string{"game"}, TablePrefix: "dnf", PVFPath: "fixture.pvf", PVFSHA256: strings.Repeat("a", 64), ItemIndexPath: "items.json", GameAPI: "http://127.0.0.1:7000/api/game/admin", GameToken: strings.Repeat("g", 32), ScanSeconds: 60}
	cfg.RegistrationProfile = "legacy-90cn"
	cfg.GameRestartSocket = filepath.Join(t.TempDir(), "restart.sock")
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	cfg.RegistrationProfile = "unknown"
	if cfg.Validate() == nil {
		t.Fatal("unknown registration profile accepted")
	}
	cfg.RegistrationProfile = ""
	cfg.GameRestartSocket = "relative.sock"
	if cfg.Validate() == nil {
		t.Fatal("relative socket accepted")
	}
}
