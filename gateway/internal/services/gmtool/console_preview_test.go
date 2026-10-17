package gmtool

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
	repomysql "dnf90.local/gateway/internal/modules/dnf/repository/mysql"
	"dnf90.local/gateway/internal/modules/dnf/testbenefits"
	platformpvf "dnf90.local/gateway/internal/platform/pvf"
	driver "github.com/go-sql-driver/mysql"
)

// Opt-in local review harness. scripts/preview_gm_live.py owns its disposable
// container and supplies all inputs; it does not read a deployment config.
func TestConsoleInteractivePreview(t *testing.T) {
	out := os.Getenv("GM_CONSOLE_PREVIEW_DIR")
	if out == "" {
		t.Skip("interactive preview is opt-in")
	}
	ctx := context.Background()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	dbConfig, err := driver.ParseDSN(os.Getenv("DNFBRIDGE_PLAYER_TEST_DSN"))
	check(err)
	if dbConfig.Net != "tcp" || !strings.HasPrefix(dbConfig.Addr, "127.0.0.1:") || dbConfig.DBName != "gm_preview_fixture" {
		t.Fatal("preview requires its disposable loopback database")
	}
	db, err := sql.Open("mysql", dbConfig.FormatDSN())
	check(err)
	defer db.Close()
	db.SetMaxOpenConns(8)
	plan := repository.DatabasePlan{ReadDatabases: []string{dbConfig.DBName}, WriteDatabases: []string{dbConfig.DBName}}
	check(repomysql.EnsureMySQLSchema(ctx, db, repomysql.SchemaOptions{AutoCreate: true, DatabasePlan: plan}))
	g, err := repomysql.NewMySQLGroup(repomysql.SQLDBAdapter{DB: db}, repomysql.MySQLGroupOptions{DatabasePlan: plan})
	check(err)
	for _, ddl := range []string{
		"CREATE TABLE web_accounts (id BIGINT PRIMARY KEY,account_id VARCHAR(128) NOT NULL UNIQUE,account VARCHAR(64) NOT NULL,created_at DATETIME(6),last_login_at DATETIME(6))",
		"CREATE TABLE web_accounts_player_accounts (account_id VARCHAR(128) PRIMARY KEY,disabled BOOLEAN NOT NULL)",
	} {
		_, err = db.Exec(ddl)
		check(err)
	}
	store, err := testbenefits.NewStore(db, "web_accounts")
	check(err)
	check(store.EnsureSchema(ctx))
	ixDir := os.Getenv("GM_CONSOLE_PREVIEW_INDEX")
	source, err := platformpvf.OpenDisk(os.Getenv("GM_CONSOLE_PREVIEW_PVF"), filepath.Join(ixDir, "pvf-directory.bin"), os.Getenv("GM_CONSOLE_PREVIEW_SHA256"))
	check(err)
	defer source.Close()
	static, err := newStaticHandler("wwwroot")
	check(err)
	s := &Server{cfg: Config{Table: "web_accounts", Password: "preview", Logger: func(string, ...any) {}}, db: db,
		deps: Deps{Repos: func() (repository.Group, bool) { return g, true }, PVF: func() (dnfpvf.Source, error) { return source, nil }, Benefits: store},
		www:  static, pvf: newPVFCache(), sessions: map[string]time.Time{}, failures: map[string][]time.Time{}}
	s.items = newItemIndex(filepath.Join(ixDir, "items.json"), s, nil)
	check(s.items.loadCache())
	quests, err := s.questReadIndex(ctx)
	check(err)
	if len(quests.Rows) < 8 {
		t.Fatal("preview needs a populated quest index")
	}
	for i, name := range []string{"晨雾", "远山", "星河", "流光"} {
		aid := fmt.Sprintf("preview:%d", i+1)
		check(g.Account.Save(ctx, repository.AccountRecord{AccountID: aid}))
		_, err = db.Exec("INSERT INTO web_accounts(id,account_id,account,created_at,last_login_at) VALUES (?,?,?,?,?)", i+1, aid, name, time.Now().UTC().AddDate(0, -1, 0), time.Now().UTC())
		check(err)
		for j := 0; j < 2; j++ {
			id := strconv.Itoa((i+1)*10 + j)
			character := repository.CharacterRecord{CharacterID: id, AccountID: aid, Slot: j, Name: name + []string{"·剑影", "·逐光"}[j], Level: 90 - i*5, Job: strconv.Itoa(j), CreatedAt: time.Now().UTC().AddDate(0, 0, -10)}
			growth, err := s.levelDefinition(ctx, character, character.Level)
			check(err)
			character.Stats = growth.Stats
			character.Stats["exp"] = int64(growth.MinimumExperience)
			check(g.Character.Save(ctx, character))
			// Current game initialization values, isolated preview characters only.
			if id != "31" {
				points, err := s.skillPointDefinition(ctx, 90-i*5)
				check(err)
				check(g.Skill.Save(ctx, repository.SkillRecord{CharacterID: id, Points: repository.SkillPointState{TotalSP: points.SP, RemainingSP: points.SP / 2, TotalTP: points.TP, RemainingTP: points.TP / 2, SyncedLevel: 90 - i*5}}))
				check(g.Settings.Save(ctx, repository.SettingsRecord{Scope: repository.CharacterContainerStateScope(id), Values: map[string]string{
					"main_list_param16": "0", "avatar_list_param16": "0", "personal_cargo_list_param16": "8", "account_cargo_selection_key": "0", "account_cargo_value32": "0", "source": "preview_current_game_defaults",
				}}))
			}

		}
	}
	ids := []int64{100050050, 100312103, 100322294, 101010864, 101030517}
	inv := repository.InventoryRecord{CharacterID: "10", Slots: map[string]repository.ItemStack{}, Warehouse: map[string]repository.ItemStack{}}
	for i := 0; i < 27; i++ {
		id := ids[i%len(ids)]
		if _, ok := s.items.lookup(uint32(id)); !ok {
			t.Fatal("missing preview item", id)
		}
		inv.Slots[fmt.Sprintf("0:%d", 9+i)] = repository.ItemStack{ItemID: id, Count: 1, Bind: i%2 == 0, Extra: map[string]string{"reinforce": strconv.Itoa(i % 13), "quality_seed": "0", "durability": "52", "max_durability": "70", "forging": "3"}}
	}
	inv.Slots["0:65"] = repository.ItemStack{ItemID: 490009735, Count: 12}
	inv.Slots["0:66"] = repository.ItemStack{ItemID: 999999990, Count: 1}
	inv.Warehouse["2:0"] = repository.ItemStack{ItemID: ids[2], Count: 1}
	check(g.Inventory.Save(ctx, inv))
	check(g.AccountInventory.Save(ctx, repository.AccountInventoryRecord{AccountID: "preview:1", Slots: map[string]repository.ItemStack{"12:0": {ItemID: ids[1], Count: 1}, "0:354": {ItemID: 3033, Count: 1200}}}))
	check(g.Equipment.Save(ctx, repository.EquipmentRecord{CharacterID: "10", Entries: map[string]repository.EquipmentEntry{"10": {SlotIndex: 10, ItemID: ids[0], Bind: true, Extra: map[string]string{"reinforce": "12", "durability": "60"}}}}))
	for _, id := range []uint32{63000, 63001, 63002} {
		entry, found := s.items.lookup(id)
		if !found || entry.Kind != "equipment" || entry.Tag != "creature" {
			t.Fatal("missing preview creature", id)
		}
	}
	petInventory, petEquipment, petRecord := petManagementFixtureRecords("20")
	check(g.Inventory.Save(ctx, petInventory))
	check(g.Equipment.Save(ctx, petEquipment))
	check(g.Pet.Save(ctx, petRecord))
	for _, id := range []uint32{63500, 64000, 64500} {
		if _, found := s.items.lookup(id); !found {
			t.Fatal("missing preview artifact", id)
		}
	}
	artifactInventory, artifactEquipment, artifactRecord := artifactManagementFixtureRecords(t, "30")
	check(g.Inventory.Save(ctx, artifactInventory))
	check(g.Equipment.Save(ctx, artifactEquipment))
	check(g.Pet.Save(ctx, artifactRecord))
	states := map[int64]repository.QuestState{}
	for i, row := range quests.Rows[:8] {
		status := "active"
		if i > 4 {
			status = "completed"
		}
		states[row.ID] = repository.QuestState{Status: status, ProgressValue: int64(i % 3), UpdatedAt: time.Now().UTC()}
	}
	check(g.Quest.Save(ctx, repository.QuestRecord{CharacterID: "10", States: states}))
	check(g.Mailbox.Save(ctx, repository.MailboxRecord{CharacterID: "10", Mails: map[string]repository.MailRecord{
		"preview-welcome": {MailID: "preview-welcome", SenderName: "运营团队", Title: "冒险启程礼包", Body: "欢迎来到阿拉德。\n这封邮件展示正文、金币和附件的查询。", Gold: 200000, CreatedAt: time.Now().UTC(), Attachments: []repository.MailAttachment{{ItemID: 490009735, Count: 3}}},
		"preview-claimed": {MailID: "preview-claimed", SenderName: "运营团队", Title: "周末补给", Body: "补给已领取。", Read: true, Claimed: true, CreatedAt: time.Now().UTC().Add(-24 * time.Hour)},
	}}))
	for _, id := range []string{"11", "20"} {
		check(g.Mailbox.Save(ctx, repository.MailboxRecord{CharacterID: id, Mails: map[string]repository.MailRecord{
			"preview-claimed": {MailID: "preview-claimed", SenderName: "运营团队", Title: "已领取的周末补给", Body: "补给已领取。", Read: true, Claimed: true, CreatedAt: time.Now().UTC()},
			"preview-letter":  {MailID: "preview-letter", SenderName: "运营团队", Title: "活动通知", Body: "周末活动已经结束。", CreatedAt: time.Now().UTC()},
			"preview-gift":    {MailID: "preview-gift", SenderName: "运营团队", Title: "待领取的活动奖励", Body: "活动奖励。", Gold: 50000, Attachments: []repository.MailAttachment{{ItemID: 490009735, Count: 2}}, CreatedAt: time.Now().UTC()},
			"preview-extra":   {MailID: "preview-extra", SenderName: "运营团队", Title: "维护通知", Body: "维护已经完成。", CreatedAt: time.Now().UTC()},
		}}))
	}
	cleanupMails := map[string]repository.MailRecord{}
	for i := 1; i <= 210; i++ {
		id := fmt.Sprintf("cleanup-%04d", i)
		m := repository.MailRecord{MailID: id, SenderName: "运营团队", Title: "历史活动通知", Body: "历史活动已结束。", Claimed: true, CreatedAt: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Minute)}
		if i > 205 {
			m.Title = "待领取活动补偿"
			m.Claimed = false
			m.Gold = 1000
		}
		cleanupMails[id] = m
	}
	check(g.Mailbox.Save(ctx, repository.MailboxRecord{CharacterID: "21", Mails: cleanupMails}))
	token, err := s.startSession()
	check(err)
	for _, scope := range []string{"account", "character"} {
		label := "账号启程福利"
		if scope == "character" {
			label = "角色成长补给"
		}
		body, _ := json.Marshal(map[string]any{"scope": scope, "itemId": "490009735", "count": 1, "title": label, "autoNewCharacters": false})
		status, result := operationCall(s, token, "benefits", string(body), "preview-rule-"+scope, nil)
		if status != 200 {
			t.Fatal(status, result)
		}
	}

	if configPath := os.Getenv("GM_GATEWAY_PREVIEW_CONFIG"); configPath != "" {
		key := os.Getenv("GM_GATEWAY_PREVIEW_KEY")
		if len(key) < 32 {
			t.Fatal("gateway preview key missing")
		}
		digest := sha256.Sum256([]byte(key))
		cfg := map[string]any{
			"certificate": filepath.Join(out, "localhost.pem"), "key": filepath.Join(out, "localhost-key.pem"),
			"management": map[string]any{
				"listen": os.Getenv("GM_CONSOLE_PREVIEW_ADDR"), "loginKeySha256": hex.EncodeToString(digest[:]), "webRoot": filepath.Join(out, "web"),
				"registrationDsn": dbConfig.FormatDSN(), "repositoryDsn": dbConfig.FormatDSN(), "accountTable": "web_accounts", "databases": []string{dbConfig.DBName}, "tablePrefix": "dnf",
				"pvfPath": os.Getenv("GM_CONSOLE_PREVIEW_PVF"), "pvfSha256": os.Getenv("GM_CONSOLE_PREVIEW_SHA256"), "pvfIndexPath": filepath.Join(ixDir, "pvf-directory.bin"), "itemIndexPath": filepath.Join(ixDir, "items.json"),
				"gameApi": os.Getenv("GM_GATEWAY_PREVIEW_GAME_API"), "gameToken": os.Getenv("GM_GATEWAY_PREVIEW_GAME_TOKEN"), "scanSeconds": 3600,
			},
		}
		raw, e := json.MarshalIndent(cfg, "", "  ")
		check(e)
		check(os.WriteFile(configPath, raw, 0600))
		return
	}
	addr := os.Getenv("GM_CONSOLE_PREVIEW_ADDR")
	host, _, err := net.SplitHostPort(addr)
	check(err)
	if host != "127.0.0.1" {
		t.Fatal("preview listener must be loopback")
	}
	ln, err := net.Listen("tcp", addr)
	check(err)
	routes := s.routes()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/console/#players", http.StatusFound)
			return
		}
		if r.URL.Path == "/console/" || r.URL.Path == "/console/index.html" {
			recorder := httptest.NewRecorder()
			routes.ServeHTTP(recorder, r)
			for k, values := range recorder.Header() {
				for _, v := range values {
					w.Header().Add(k, v)
				}
			}
			w.Header().Del("Content-Length")
			body := strings.Replace(recorder.Body.String(), "<div class=\"topbar-actions\">", "<div class=\"topbar-actions\"><span class=\"preview-badge\">本地预览 · 示例玩家</span>", 1)
			w.WriteHeader(recorder.Code)
			_, _ = w.Write([]byte(body))
			return
		}
		routes.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
	defer server.Close()
	done := make(chan error, 1)
	go func() { done <- server.Serve(ln) }()
	ready, _ := json.Marshal(map[string]any{"url": "http://" + ln.Addr().String() + "/console/#players", "pvfChecksum": itemIndexSourceChecksum(source), "items": len(s.items.entries), "quests": len(quests.Rows), "sampleAccount": "preview:1", "sampleCharacter": "10", "data": "disposable synthetic players; real PVF definitions"})
	check(os.WriteFile(filepath.Join(out, "ready.json"), ready, 0600))
	t.Log("preview ready", ln.Addr().String())
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	expires := time.NewTimer(12 * time.Hour)
	defer expires.Stop()
	for {
		select {
		case err := <-done:
			if err != http.ErrServerClosed {
				t.Fatal(err)
			}
			return
		case <-expires.C:
			return
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(out, "stop")); err == nil {
				return
			}
		}
	}
}
