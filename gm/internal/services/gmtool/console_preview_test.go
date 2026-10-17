package gmtool

import (
	"context"
	"database/sql"
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

	dnfpvf "github.com/Cec1c/DNF90GM/internal/modules/dnf/pvf"
	"github.com/Cec1c/DNF90GM/internal/modules/dnf/repository"
	repomysql "github.com/Cec1c/DNF90GM/internal/modules/dnf/repository/mysql"
	"github.com/Cec1c/DNF90GM/internal/modules/dnf/testbenefits"
	platformpvf "github.com/Cec1c/DNF90GM/internal/platform/pvf"
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
	static, err := newStaticHandler("")
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
	for i, name := range []string{"晨雾", "远山", "星河"} {
		aid := fmt.Sprintf("preview:%d", i+1)
		check(g.Account.Save(ctx, repository.AccountRecord{AccountID: aid}))
		_, err = db.Exec("INSERT INTO web_accounts(id,account_id,account,created_at,last_login_at) VALUES (?,?,?,?,?)", i+1, aid, name, time.Now().UTC().AddDate(0, -1, 0), time.Now().UTC())
		check(err)
		for j := 0; j < 2; j++ {
			id := fmt.Sprintf("%04d", (i+1)*10+j)
			check(g.Character.Save(ctx, repository.CharacterRecord{CharacterID: id, AccountID: aid, Slot: j, Name: name + []string{"·剑影", "·逐光"}[j], Level: 90 - i*5, Job: strconv.Itoa(j), CreatedAt: time.Now().UTC().AddDate(0, 0, -10)}))
		}
	}
	ids := []int64{100050050, 100312103, 100322294, 101010864, 101030517}
	inv := repository.InventoryRecord{CharacterID: "0010", Slots: map[string]repository.ItemStack{}, Warehouse: map[string]repository.ItemStack{}}
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
	check(g.Equipment.Save(ctx, repository.EquipmentRecord{CharacterID: "0010", Entries: map[string]repository.EquipmentEntry{"10": {SlotIndex: 10, ItemID: ids[0], Bind: true, Extra: map[string]string{"reinforce": "12", "durability": "60"}}}}))
	states := map[int64]repository.QuestState{}
	for i, row := range quests.Rows[:8] {
		status := "active"
		if i > 4 {
			status = "completed"
		}
		states[row.ID] = repository.QuestState{Status: status, ProgressValue: int64(i % 3), UpdatedAt: time.Now().UTC()}
	}
	check(g.Quest.Save(ctx, repository.QuestRecord{CharacterID: "0010", States: states}))
	check(g.Mailbox.Save(ctx, repository.MailboxRecord{CharacterID: "0010", Mails: map[string]repository.MailRecord{
		"preview-welcome": {MailID: "preview-welcome", SenderName: "运营团队", Title: "冒险启程礼包", Body: "欢迎来到阿拉德。\n这封邮件展示正文、金币和附件的查询。", Gold: 200000, CreatedAt: time.Now().UTC(), Attachments: []repository.MailAttachment{{ItemID: 490009735, Count: 3}}},
		"preview-claimed": {MailID: "preview-claimed", SenderName: "运营团队", Title: "周末补给", Body: "补给已领取。", Read: true, Claimed: true, CreatedAt: time.Now().UTC().Add(-24 * time.Hour)},
	}}))
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
	ready, _ := json.Marshal(map[string]any{"url": "http://" + ln.Addr().String() + "/console/#players", "pvfChecksum": itemIndexSourceChecksum(source), "items": len(s.items.entries), "quests": len(quests.Rows), "sampleAccount": "preview:1", "sampleCharacter": "0010", "data": "disposable synthetic players; real PVF definitions"})
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
