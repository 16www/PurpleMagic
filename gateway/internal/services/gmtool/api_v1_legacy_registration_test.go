package gmtool

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/repository"
	repomysql "dnf90.local/gateway/internal/modules/dnf/repository/mysql"
	driver "github.com/go-sql-driver/mysql"
)

// TestV1LegacyRegistrationSelectSQLShape pins the two registration profiles at
// the SQL boundary. The MySQL test below proves the legacy profile against a
// real web_accounts table without _player_accounts.
func TestV1LegacyRegistrationSelectSQLShape(t *testing.T) {
	const table = "dnf90_web_accounts"
	legacy := &Server{cfg: Config{Table: table, RegistrationProfile: "legacy-90cn"}}
	wantLegacy := "SELECT a.id,a.account_id,a.account,0,a.created_at,a.last_login_at FROM `dnf90_web_accounts` a"
	if got := legacy.v1AccountSelect(); got != wantLegacy {
		t.Fatalf("legacy account select = %q, want %q", got, wantLegacy)
	}
	if got := legacy.v1AccountSelect(); strings.Contains(strings.ToUpper(got), " JOIN ") || strings.Contains(got, "_player_accounts") || strings.Contains(got, "p.") {
		t.Fatalf("legacy account select references a missing disabled-policy relation: %q", got)
	}
	if got := legacy.v1AccountDisabledExpression(); got != "0" {
		t.Fatalf("legacy disabled expression = %q, want literal 0", got)
	}

	modern := &Server{cfg: Config{Table: table}}
	wantModern := "SELECT a.id,a.account_id,a.account,COALESCE(p.disabled,0),a.created_at,a.last_login_at FROM `dnf90_web_accounts` a LEFT JOIN `dnf90_web_accounts_player_accounts` p ON p.account_id=BINARY a.account_id"
	if got := modern.v1AccountSelect(); got != wantModern {
		t.Fatalf("default account select = %q, want %q", got, wantModern)
	}
	if got := modern.v1AccountDisabledExpression(); got != "COALESCE(p.disabled,0)" {
		t.Fatalf("default disabled expression = %q", got)
	}
}

type v1LegacyAccountFixture struct {
	PanelID      int64
	AccountID    string
	Name         string
	Roles        int
	DeletedRoles int
	LastLoginAt  time.Time
}

// TestV1LegacyRegistrationMySQL exercises the 90CN profile against the actual
// old registration schema: one web_accounts table and no
// web_accounts_player_accounts relation. It creates its own uniquely named
// disposable database under DNFBRIDGE_PLAYER_TEST_DSN and removes it afterward.
func TestV1LegacyRegistrationMySQL(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("DNFBRIDGE_PLAYER_TEST_DSN"))
	if dsn == "" {
		t.Skip("disposable MySQL DSN not configured")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.PingContext(ctx); err != nil {
		t.Fatalf("ping disposable MySQL: %v", err)
	}

	name := fmt.Sprintf("gm_v1_legacy_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+name+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatalf("create disposable database %s: %v", name, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP DATABASE IF EXISTS `"+name+"`"); err != nil {
			t.Errorf("drop disposable database %s: %v", name, err)
		}
	})

	cfg, err := driver.ParseDSN(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.DBName = name
	cfg.ParseTime = true
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping legacy fixture database: %v", err)
	}

	plan := repository.DatabasePlan{ReadDatabases: []string{name}, WriteDatabases: []string{name}}
	if err := repomysql.EnsureMySQLSchema(ctx, db, repomysql.SchemaOptions{AutoCreate: true, DatabasePlan: plan}); err != nil {
		t.Fatal(err)
	}
	group, err := repomysql.NewMySQLGroupFromDB(db, repomysql.MySQLGroupOptions{DatabasePlan: plan})
	if err != nil {
		t.Fatal(err)
	}

	// Matches the discovered 90CN registration schema. The disabled-policy side
	// table is deliberately not created.
	if _, err := db.ExecContext(ctx, `CREATE TABLE web_accounts (
		id BIGINT NOT NULL PRIMARY KEY,
		account VARCHAR(64) NOT NULL,
		password_hash VARCHAR(255) NOT NULL,
		account_id VARCHAR(128) NOT NULL,
		created_at DATETIME NOT NULL,
		last_login_at DATETIME NULL,
		last_login_ip VARCHAR(64) NULL,
		UNIQUE KEY uq_web_accounts_account_id (account_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`); err != nil {
		t.Fatal(err)
	}

	fixtures := []v1LegacyAccountFixture{
		{1, "dnf:legacy01", "player01", 3, 1, time.Time{}},
		{2, "dnf:legacy02", "player02", 2, 0, time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)},
		{3, "dnf:legacy03", "player03", 0, 0, time.Time{}},
		{4, "dnf:legacy04", "player04", 1, 0, time.Time{}},
		{5, "dnf:legacy05", "player05", 1, 0, time.Time{}},
		{6, "dnf:legacy06", "player06", 1, 0, time.Time{}},
		{7, "DNF:CaseProbe", "CaseProbe", 1, 0, time.Time{}},
		{8, "dnf:legacy08", "player08", 1, 0, time.Time{}},
		{9, "dnf:legacy09", "player09", 1, 0, time.Time{}},
		{10, "dnf:legacy10", "player10", 1, 0, time.Time{}},
		{11, "dnf:legacy11", "player11", 1, 0, time.Time{}},
		{12, "dnf:legacy12", "player12", 1, 0, time.Time{}},
		{13, "dnf:legacy13", "player13", 1, 0, time.Time{}},
		{14, "dnf:legacy14", "player14", 1, 0, time.Time{}},
		{15, "dnf:legacy15", "player15", 1, 0, time.Time{}},
	}
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, fixture := range fixtures {
		var lastLogin any
		if !fixture.LastLoginAt.IsZero() {
			lastLogin = fixture.LastLoginAt
		}
		if _, err := db.ExecContext(ctx, "INSERT INTO web_accounts(id,account,password_hash,account_id,created_at,last_login_at,last_login_ip) VALUES (?,?,?,?,?,?,NULL)", fixture.PanelID, fixture.Name, "fixture", fixture.AccountID, createdAt, lastLogin); err != nil {
			t.Fatalf("insert account %s: %v", fixture.AccountID, err)
		}
		for slot := 0; slot < fixture.Roles; slot++ {
			record := repository.CharacterRecord{
				CharacterID: fmt.Sprintf("%04d", fixture.PanelID*10+int64(slot)),
				AccountID:   fixture.AccountID,
				Slot:        slot,
				Name:        fmt.Sprintf("role-%02d-%d", fixture.PanelID, slot),
				Level:       90,
				Job:         "0",
				CreatedAt:   createdAt,
			}
			if err := group.Character.Save(ctx, record); err != nil {
				t.Fatalf("save role for %s: %v", fixture.AccountID, err)
			}
		}
		for deleted := 0; deleted < fixture.DeletedRoles; deleted++ {
			record := repository.CharacterRecord{
				CharacterID: fmt.Sprintf("%04d", fixture.PanelID*10+9+int64(deleted)),
				AccountID:   fixture.AccountID,
				Slot:        fixture.Roles + deleted,
				Name:        fmt.Sprintf("deleted-%02d-%d", fixture.PanelID, deleted),
				Level:       90,
				Job:         "0",
				CreatedAt:   createdAt,
				Stats:       map[string]int64{"delete_flag": 1},
			}
			if err := group.Character.Save(ctx, record); err != nil {
				t.Fatalf("save deleted role for %s: %v", fixture.AccountID, err)
			}
		}
	}

	var auxiliaryTables int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?", "web_accounts_player_accounts").Scan(&auxiliaryTables); err != nil {
		t.Fatal(err)
	}
	if auxiliaryTables != 0 {
		t.Fatal("legacy fixture unexpectedly contains web_accounts_player_accounts")
	}
	var accountRows int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM web_accounts").Scan(&accountRows); err != nil {
		t.Fatal(err)
	}
	if accountRows != len(fixtures) {
		t.Fatalf("account rows = %d, want %d", accountRows, len(fixtures))
	}

	s := &Server{
		cfg:      Config{Table: "web_accounts", Password: "fixture", RegistrationProfile: "legacy-90cn"},
		db:       db,
		deps:     Deps{Repos: func() (repository.Group, bool) { return group, true }},
		www:      http.NotFoundHandler(),
		sessions: map[string]time.Time{},
		failures: map[string][]time.Time{},
	}
	token, err := s.startSession()
	if err != nil {
		t.Fatal(err)
	}

	byPanel := make(map[string]v1LegacyAccountFixture, len(fixtures))
	for _, fixture := range fixtures {
		byPanel[strconv.FormatInt(fixture.PanelID, 10)] = fixture
	}
	seen := make(map[string]bool, len(fixtures))
	lastPanel := int64(-1)
	query := "limit=5"
	for page := 0; ; page++ {
		if page > 10 {
			t.Fatal("legacy account pagination did not terminate")
		}
		accounts, next := v1LegacyAccountPage(t, s, token, query)
		for _, account := range accounts {
			panelID, ok := account["panelId"].(string)
			if !ok {
				t.Fatalf("panelId type = %T", account["panelId"])
			}
			if seen[panelID] {
				t.Fatalf("duplicate account across pages: %s", panelID)
			}
			seen[panelID] = true
			panel, err := strconv.ParseInt(panelID, 10, 64)
			if err != nil || panel <= lastPanel {
				t.Fatalf("pagination order = %d after %d (%v)", panel, lastPanel, err)
			}
			lastPanel = panel
			fixture, ok := byPanel[panelID]
			if !ok {
				t.Fatalf("unexpected account row %s", panelID)
			}
			if account["accountId"] != fixture.AccountID || account["name"] != fixture.Name || account["disabled"] != false {
				t.Fatalf("account row = %+v, want %+v", account, fixture)
			}
			count, ok := account["characterCount"].(float64)
			if !ok || int(count) != fixture.Roles {
				t.Fatalf("characterCount for %s = %v, want %d", fixture.AccountID, account["characterCount"], fixture.Roles)
			}
		}
		if next == "" {
			break
		}
		query = "limit=5&cursor=" + url.QueryEscape(next)
	}
	if len(seen) != len(fixtures) {
		t.Fatalf("pagination saw %d accounts, want %d", len(seen), len(fixtures))
	}

	searched, next := v1LegacyAccountPage(t, s, token, "q="+url.QueryEscape("dnf:casePROBE")+"&limit=100")
	if next != "" || len(searched) != 1 {
		t.Fatalf("case-insensitive account-id search returned %d rows (next=%q)", len(searched), next)
	}
	if searched[0]["accountId"] != "DNF:CaseProbe" {
		t.Fatalf("case-insensitive account-id search returned %+v", searched[0])
	}

	enabled, next := v1LegacyAccountPage(t, s, token, "state=enabled&limit=100")
	if next != "" || len(enabled) != len(fixtures) {
		t.Fatalf("enabled filter returned %d rows (next=%q), want %d", len(enabled), next, len(fixtures))
	}
	for _, account := range enabled {
		if account["disabled"] != false {
			t.Fatalf("legacy enabled filter returned disabled row: %+v", account)
		}
	}
	disabled, next := v1LegacyAccountPage(t, s, token, "state=disabled&limit=100")
	if next != "" || len(disabled) != 0 {
		t.Fatalf("legacy disabled filter returned %d rows (next=%q), want 0", len(disabled), next)
	}

	status, out := v1TestCall(t, s, token, "GET", "/api/v1/accounts/"+url.PathEscape("dnf:legacy01"))
	if status != 200 {
		t.Fatalf("legacy account detail status = %d, body=%v", status, out)
	}
	detail, ok := out["data"].(map[string]any)
	if !ok {
		t.Fatalf("legacy account detail data = %T", out["data"])
	}
	if detail["accountId"] != "dnf:legacy01" || detail["name"] != "player01" || detail["disabled"] != false || detail["createdAt"] == nil || detail["lastLoginAt"] != nil {
		t.Fatalf("legacy account detail = %+v", detail)
	}
	if count, ok := detail["characterCount"].(float64); !ok || int(count) != 3 {
		t.Fatalf("legacy account detail characterCount = %v, want 3", detail["characterCount"])
	}
	status, out = v1TestCall(t, s, token, "GET", "/api/v1/accounts/"+url.PathEscape("dnf:legacy01")+"/characters")
	if status != 200 {
		t.Fatalf("legacy account characters status = %d, body=%v", status, out)
	}
	roles, ok := out["data"].([]any)
	if !ok || len(roles) != 3 {
		t.Fatalf("legacy account roles = %v, want 3 active roles", out["data"])
	}
	status, out = v1TestCall(t, s, token, "GET", "/api/v1/accounts/dnf%3Amissing")
	if status != 404 {
		t.Fatalf("missing legacy account status = %d, body=%v", status, out)
	}
}

func v1LegacyAccountPage(t *testing.T, s *Server, token, query string) ([]map[string]any, string) {
	t.Helper()
	path := "/api/v1/accounts"
	if query != "" {
		path += "?" + query
	}
	status, out := v1TestCall(t, s, token, "GET", path)
	if status != 200 {
		t.Fatalf("legacy accounts %q status = %d, body=%v", query, status, out)
	}
	raw, ok := out["data"].([]any)
	if !ok {
		t.Fatalf("legacy accounts data = %T", out["data"])
	}
	accounts := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		account, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("legacy account row = %T", item)
		}
		accounts = append(accounts, account)
	}
	page, ok := out["page"].(map[string]any)
	if !ok {
		t.Fatalf("legacy accounts page = %T", out["page"])
	}
	next, _ := page["nextCursor"].(string)
	return accounts, next
}
