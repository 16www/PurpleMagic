package gmtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/modules/dnf/adminops"
	"dnf90.local/gateway/internal/modules/dnf/progression"
	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

type pointFailStore struct {
	repository.SkillPointManagementRepository
}

func (s pointFailStore) SaveSkillPointManagement(ctx context.Context, v repository.SkillPointManagementState) error {
	if err := s.SkillPointManagementRepository.SaveSkillPointManagement(ctx, v); err != nil {
		return err
	}
	return errors.New("injected after save")
}

type pointFailUOW struct {
	repository.SkillPointManagementUnitOfWork
	save bool
}

func (s pointFailUOW) WithinSkillPointManagement(ctx context.Context, a, id string, fn func(repository.SkillPointManagementRepository, repository.ManagementReceiptStore) error) error {
	return s.SkillPointManagementUnitOfWork.WithinSkillPointManagement(ctx, a, id, func(r repository.SkillPointManagementRepository, receipts repository.ManagementReceiptStore) error {
		if s.save {
			r = pointFailStore{r}
		} else {
			receipts = failingReceipts{receipts}
		}
		return fn(r, receipts)
	})
}

func TestV1SkillPointsMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			s, _, open := currencyFixture(t, shards)
			ctx := context.Background()
			g := open()
			check := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			source := newItemCacheTestSource(readonlyTestSource{progression.SkillPointTablePath: "[sp table]\n1 100\n2 200\n3 300\n[/sp table]\n[tp table]\n1 2\n2 3\n3 4\n[/tp table]\n"})
			s.pvf = newPVFCache()
			s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
			def, e := s.skillPointDefinition(ctx, 2)
			check(e)
			if def.SP != 300 || def.TP != 5 {
				t.Fatal(def)
			}
			for i, id := range []string{"10", "11", "20", "21"} {
				a := "wallet:1"
				if i > 1 {
					a = "wallet:2"
				}
				check(g.Character.Save(ctx, repository.CharacterRecord{CharacterID: id, AccountID: a, Slot: i, Name: "点数" + id, Job: "0", Level: 2, Stats: map[string]int64{"bonus_sp": 40, "bonus_tp": 3, "gold": 777, "keep": 988}}))
				if id == "21" {
					continue
				}
				check(g.Skill.Save(ctx, repository.SkillRecord{CharacterID: id, Points: repository.SkillPointState{TotalSP: 340, RemainingSP: 120, TotalTP: 8, RemainingTP: 3, SyncedLevel: 2}, Skills: map[int64]repository.SkillState{42: {Level: 3, Enabled: true}}, Layouts: map[int]repository.SkillLayout{0: {1: 42}, 1: {2: 42}}, Cooldowns: map[int64]time.Time{42: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}}))
			}
			var database string
			check(s.db.QueryRow("SELECT DATABASE()").Scan(&database))
			execShards := func(query string, args ...any) {
				t.Helper()
				for i := 0; i < shards; i++ {
					db := fmt.Sprintf("`%s%d`", strings.TrimSuffix(database, "0"), i)
					_, e := s.db.Exec(strings.ReplaceAll(query, "DB", db), args...)
					check(e)
				}
			}
			execShards("INSERT INTO DB.dnf_character_stats(character_id,stat_key,stat_value) SELECT character_id,'negative_sentinel',-7 FROM DB.dnf_characters")
			u := g.Character.(repository.SkillPointManagementUnitOfWork)
			read := func(id string) repository.SkillPointManagementState {
				v, e := open().Character.(repository.SkillPointManagementReader).LoadSkillPointManagement(ctx, id)
				check(e)
				return v
			}
			full := func(id string) (repository.CharacterRecord, repository.SkillRecord) {
				c, ok, e := g.Character.Load(ctx, id)
				check(e)
				if !ok {
					t.Fatal("missing character")
				}
				k, _, e := g.Skill.Load(ctx, id)
				check(e)
				return c, k
			}
			c0, k0 := full("10")
			otherC, otherK := full("11")
			a0, _, e := g.Account.Load(ctx, "wallet:1")
			check(e)
			start := read("10")
			if len(start.Character.Stats) != 4 || start.Skill.Skills != nil || start.Skill.Layouts != nil || start.Skill.Cooldowns != nil {
				t.Fatal("unbounded read")
			}
			cmd := adminops.SkillPointCommand{AccountID: "wallet:1", CharacterID: "10", Action: "add", Target: "sp", Amount: "60", Revision: adminops.SkillPointRevision(start), DefinitionRevision: adminops.SkillPointDefinitionRevision(def), Reason: "技能点补偿", Actor: "管理员", Key: "skill-points-same-key"}
			offline := func(context.Context, string) error { return nil }
			run := func(c adminops.SkillPointCommand) (adminops.SkillPointReceipt, error) {
				return adminops.RunSkillPointOperation(ctx, u, c, true, s.skillPointDefinition, offline)
			}
			unchanged := func(n int) {
				t.Helper()
				c, k := full("10")
				if !reflect.DeepEqual(c, c0) || !reflect.DeepEqual(k, k0) {
					t.Fatal("unexpected field mutation")
				}
				r, e := u.SkillPointManagementLog(ctx, "10", 0, 100)
				check(e)
				if len(r) != n {
					t.Fatal("unexpected receipts", len(r))
				}
			}
			plan, e := adminops.RunSkillPointOperation(ctx, u, cmd, false, s.skillPointDefinition, nil)
			check(e)
			if plan.After.BonusSP != 100 || plan.After.RemainingSP != 180 || plan.After.RemainingTP != 3 {
				t.Fatal(plan)
			}
			unchanged(0)
			for _, mode := range []string{"save", "receipt", "online", "late-online", "unavailable"} {
				worker := u
				off := offline
				switch mode {
				case "save":
					worker = pointFailUOW{u, true}
				case "receipt":
					worker = pointFailUOW{u, false}
				case "online":
					off = func(context.Context, string) error { return adminops.ErrCharacterOnline }
				case "unavailable":
					off = nil
				case "late-online":
					calls := 0
					off = func(context.Context, string) error {
						calls++
						if calls == 2 {
							return adminops.ErrCharacterOnline
						}
						return nil
					}
				}
				if _, e := adminops.RunSkillPointOperation(ctx, worker, cmd, true, s.skillPointDefinition, off); e == nil {
					t.Fatal(mode)
				}
				unchanged(0)
			}
			var wg sync.WaitGroup
			errs := make(chan error, 6)
			for range 6 {
				wg.Add(1)
				go func() { defer wg.Done(); _, e := run(cmd); errs <- e }()
			}
			wg.Wait()
			close(errs)
			for e := range errs {
				check(e)
			}
			c, k := full("10")
			if c.Stats["bonus_sp"] != 100 || c.Stats["bonus_tp"] != 3 || c.Stats["gm_skill_points_sequence_v1"] != 1 || k.Points.RemainingSP != 180 {
				t.Fatal(c.Stats, k.Points)
			}
			preserved := repository.CloneCharacter(c)
			preserved.Stats["bonus_sp"] = 40
			delete(preserved.Stats, "gm_skill_points_sequence_v1")
			preserved.UpdatedAt = c0.UpdatedAt
			kept := repository.CloneSkill(k)
			kept.Points = k0.Points
			kept.UpdatedAt = k0.UpdatedAt
			if !reflect.DeepEqual(preserved, c0) || !reflect.DeepEqual(kept, k0) {
				t.Fatal("unrelated character/skill fields changed")
			}
			oc, ok := full("11")
			a1, _, e := g.Account.Load(ctx, "wallet:1")
			check(e)
			if !reflect.DeepEqual(oc, otherC) || !reflect.DeepEqual(ok, otherK) || !reflect.DeepEqual(a0, a1) {
				t.Fatal("other scope changed")
			}
			recovered, e := adminops.RunSkillPointOperation(ctx, open().Character.(repository.SkillPointManagementUnitOfWork), cmd, true, nil, nil)
			check(e)
			if recovered.After != plan.After {
				t.Fatal("recovery")
			}
			bad := cmd
			bad.Amount = "61"
			if _, e = run(bad); !errors.Is(e, adminops.ErrIdempotency) {
				t.Fatal(e)
			}
			bad = cmd
			bad.Key = "skill-stale-other-key"
			if _, e = run(bad); !errors.Is(e, adminops.ErrConflict) {
				t.Fatal(e)
			}
			// Same revision, distinct requests: only one transaction can advance the ledger.
			cmd.Revision = adminops.SkillPointRevision(read("10"))
			errs = make(chan error, 4)
			for i := range 4 {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					v := cmd
					v.Key = fmt.Sprintf("skill-distinct-key-%d", i)
					_, e := run(v)
					errs <- e
				}(i)
			}
			wg.Wait()
			close(errs)
			n := 0
			for e := range errs {
				if e == nil {
					n++
				} else if !errors.Is(e, adminops.ErrConflict) {
					t.Fatal(e)
				}
			}
			if n != 1 {
				t.Fatal(n)
			}
			operation := func(action, target, amount string) adminops.SkillPointReceipt {
				t.Helper()
				cmd.Action, cmd.Target, cmd.Amount = action, target, amount
				cmd.Revision = adminops.SkillPointRevision(read("10"))
				cmd.Key = fmt.Sprintf("skill-point-%d", time.Now().UnixNano())
				v, e := run(cmd)
				check(e)
				return v
			}
			if r := operation("set", "tp", "10"); r.After.TotalTP != 15 || r.After.RemainingTP != 10 {
				t.Fatal(r)
			}
			if r := operation("subtract", "tp", "2"); r.After.BonusTP != 8 || r.After.RemainingTP != 8 {
				t.Fatal(r)
			}
			if r := operation("set", "sp", "65235"); r.After.TotalSP != 65535 {
				t.Fatal(r)
			}
			operation("set", "sp", "0")
			if r := operation("clear_remaining", "sp", "0"); r.After.RemainingSP != 0 || r.After.BonusSP != 0 || r.After.TotalSP != 300 {
				t.Fatal(r)
			}
			if r := operation("sync", "both", "0"); r.After.RemainingSP != 0 {
				t.Fatal("sync refunded consumed points", r)
			}
			operation("clear_remaining", "tp", "0")
			cmd.Revision = adminops.SkillPointRevision(read("10"))
			cmd.Action, cmd.Target, cmd.Amount = "set", "tp", "0"
			cmd.Key = "skill-below-spent-key"
			if _, e = run(cmd); !errors.Is(e, adminops.ErrSkillPointSpent) {
				t.Fatal("below spent", e)
			}
			for _, r := range [][3]string{{"add", "sp", "0"}, {"set", "sp", "-1"}, {"set", "sp", "01"}, {"set", "sp", "65536"}, {"set", "sp", "65236"}, {"subtract", "sp", "1"}, {"sync", "sp", "0"}, {"clear_remaining", "sp", "1"}, {"add", "both", "1"}} {
				bad = cmd
				bad.Action, bad.Target, bad.Amount = r[0], r[1], r[2]
				if _, e = run(bad); !errors.Is(e, adminops.ErrSkillPointInput) {
					t.Fatal(r, e)
				}
			}
			bad = cmd
			bad.CharacterID = "21"
			bad.AccountID = "wallet:2"
			bad.Revision = adminops.SkillPointRevision(read("21"))
			if _, e = run(bad); !errors.Is(e, adminops.ErrSkillPointMissing) {
				t.Fatal("missing ledger", e)
			}
			bad = cmd
			bad.AccountID = "wallet:2"
			if _, e = run(bad); e == nil {
				t.Fatal("wrong owner")
			}
			// The real game's skill-only transaction holds the same parent row.
			locked := make(chan struct{})
			release := make(chan struct{})
			gameDone := make(chan error, 1)
			go func() {
				gameDone <- g.CharacterSkills.WithinCharacterSkill(ctx, "10", func(repo repository.SkillRepository) error {
					v, _, e := repo.Load(ctx, "10")
					if e != nil {
						return e
					}
					close(locked)
					<-release
					v.Points.RemainingTP = 1
					return repository.SaveSkillFields(ctx, repo, v, repository.SkillFieldPoints)
				})
			}()
			select {
			case <-locked:
			case e := <-gameDone:
				t.Fatal(e)
			case <-time.After(5 * time.Second):
				t.Fatal("game lock timeout")
			}
			bad = cmd
			bad.Action, bad.Target, bad.Amount = "sync", "both", "0"
			done := make(chan error, 1)
			go func() { _, e := run(bad); done <- e }()
			select {
			case e := <-done:
				close(release)
				t.Fatal("did not wait for game", e)
			case <-time.After(100 * time.Millisecond):
			}
			close(release)
			check(<-gameDone)
			if e = <-done; !errors.Is(e, adminops.ErrConflict) {
				t.Fatal("game write conflict", e)
			}
			// HTTP signatures, definition generations, permanent replay and cursor scopes.
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(adminapi.Response{Version: 1, OK: true})
			}))
			defer runtime.Close()
			s.deps.Runtime, e = adminapi.NewClient(runtime.URL+adminapi.Path, strings.Repeat("x", 32))
			check(e)
			token, e := s.startSession()
			check(e)
			status, body := v1TestCall(t, s, token, "GET", "/api/v1/characters/10/skill-points")
			if status != 200 {
				t.Fatal(status, body)
			}
			data := body["data"].(map[string]any)
			payload := map[string]any{"accountId": "wallet:1", "action": "add", "target": "sp", "amount": "10", "revision": data["revision"], "definitionRevision": data["definitionRevision"], "reason": "HTTP补偿"}
			call := func(path, key string, headers map[string]string) (int, map[string]any) {
				raw, _ := json.Marshal(payload)
				return operationCall(s, token, path, string(raw), key, headers)
			}
			path := "characters/10/skill-point-operations"
			status, _ = call(path+"/preview", "", map[string]string{"X-CSRF-Token": ""})
			if status != 403 {
				t.Fatal(status)
			}
			status, body = call(path+"/preview", "", nil)
			if status != 200 {
				t.Fatal(status, body)
			}
			payload["planToken"] = body["data"].(map[string]any)["planToken"]
			validToken := payload["planToken"]
			payload["amount"] = "11"
			status, _ = call(path, "skill-http-request-001", nil)
			if status != 409 {
				t.Fatal("tamper", status)
			}
			payload["amount"] = "10"
			identity := adminops.SkillPointCommand{AccountID: "wallet:1", CharacterID: "10", Action: "add", Target: "sp", Amount: "10", Revision: data["revision"].(string), DefinitionRevision: data["definitionRevision"].(string), Reason: "HTTP补偿", Actor: "管理员"}
			for _, at := range []time.Time{time.Now().Add(-11 * time.Minute), time.Now().Add(time.Minute)} {
				payload["planToken"] = s.v1CursorToken("skill-points-plan", adminops.SkillPointIdentity(identity), fmt.Sprint(at.Unix()))
				status, _ = call(path, "skill-http-request-001", nil)
				if status != 409 {
					t.Fatal("expiry", status)
				}
			}
			payload["planToken"] = validToken
			oldChecksum := source.checksum
			source.checksum = strings.Repeat("a", 64)
			status, _ = call(path, "skill-http-request-001", nil)
			if status != 409 {
				t.Fatal("PVF generation", status)
			}
			source.checksum = oldChecksum
			status, body = call(path, "skill-http-request-001", nil)
			if status != 200 {
				t.Fatal(status, body)
			}
			receipt := body["data"]
			s.deps.Runtime = nil
			s.deps.PVF = nil
			status, body = call(path, "skill-http-request-001", nil)
			if status != 200 || !reflect.DeepEqual(body["data"], receipt) {
				t.Fatal("replay without runtime/PVF", status, body)
			}
			status, body = v1TestCall(t, s, token, "GET", "/api/v1/"+path+"?limit=1")
			if status != 200 {
				t.Fatal(status, body)
			}
			cursor := url.QueryEscape(body["page"].(map[string]any)["nextCursor"].(string))
			status, _ = v1TestCall(t, s, token, "GET", "/api/v1/"+path+"?limit=1&cursor="+cursor)
			if status != 200 {
				t.Fatal(status)
			}
			for _, query := range []string{"characters/11/skill-point-operations?limit=1", path + "?limit=2"} {
				status, _ = v1TestCall(t, s, token, "GET", "/api/v1/"+query+"&cursor="+cursor)
				if status != 400 {
					t.Fatal("cursor binding", status)
				}
			}
			// Default authoritative columns ignore shadow stats; legacy mode reads and updates them.
			execShards("INSERT INTO DB.dnf_character_stats(character_id,stat_key,stat_value) SELECT character_id,'bonus_sp',41 FROM DB.dnf_characters WHERE character_id='20'")
			if read("20").Character.Stats["bonus_sp"] != 40 {
				t.Fatal("shadow won")
			}
			t.Run("legacy-mirror", func(t *testing.T) {
				t.Setenv("DNFBRIDGE_CHARACTER_MIRROR_STATS_AUTHORITATIVE", "off")
				v := read("20")
				if v.Character.Stats["bonus_sp"] != 41 || v.Character.Stats["bonus_tp"] != 3 {
					t.Fatal("legacy projection")
				}
				c := cmd
				c.CharacterID, c.AccountID = "20", "wallet:2"
				c.Action, c.Target, c.Amount = "add", "sp", "1"
				c.Revision = adminops.SkillPointRevision(v)
				c.Key = "skill-legacy-request"
				_, e := adminops.RunSkillPointOperation(ctx, u, c, true, func(context.Context, int) (adminops.SkillPointDefinition, error) { return def, nil }, offline)
				check(e)
				fullC, _ := full("20")
				if read("20").Character.Stats["bonus_sp"] != 42 || fullC.Stats["bonus_sp"] != 42 {
					t.Fatal("legacy write")
				}
			})
			if read("20").Character.Stats["bonus_sp"] != 42 {
				t.Fatal("column not updated")
			}
			for _, column := range []string{"bonus_sp", "bonus_tp"} {
				execShards("UPDATE DB.dnf_characters SET " + column + "=-1 WHERE character_id='20'")
				if _, e := adminops.SkillPointValues(read("20")); !errors.Is(e, adminops.ErrSkillPointLedger) {
					t.Fatal("corrupt column", e)
				}
				execShards("UPDATE DB.dnf_characters SET " + column + "=0 WHERE character_id='20'")
			}
			execShards("UPDATE DB.dnf_skills SET remaining_sp=total_sp+1 WHERE character_id='20'")
			if _, e := adminops.SkillPointValues(read("20")); !errors.Is(e, adminops.ErrSkillPointLedger) {
				t.Fatal("corrupt ledger", e)
			}
		})
	}
}

func TestV1SkillPointDefinitionSingleFile(t *testing.T) {
	source := newItemCacheTestSource(readonlyTestSource{progression.SkillPointTablePath: "[sp table]\n1 10\n2 20\n[/sp table]\n[tp table]\n2 1\n[/tp table]\n"})
	s := &Server{pvf: newPVFCache(), deps: Deps{PVF: func() (dnfpvf.Source, error) { return source, nil }}}
	for _, level := range []int{1, 2, 1, 2} {
		d, e := s.skillPointDefinition(context.Background(), level)
		if e != nil || d.SP != map[int]int{1: 10, 2: 30}[level] {
			t.Fatal(d, e)
		}
	}
	if source.reads != 1 {
		t.Fatal("static table not reused", source.reads)
	}
	source.checksum = strings.Repeat("a", 64)
	source.readonlyTestSource[progression.SkillPointTablePath] = "[sp table]\n1 15\n2 20\n[/sp table]\n[tp table]\n2 1\n[/tp table]\n"
	d, e := s.skillPointDefinition(context.Background(), 2)
	if e != nil || d.SP != 35 || source.reads != 2 {
		t.Fatal("PVF not invalidated", d, e, source.reads)
	}
}
