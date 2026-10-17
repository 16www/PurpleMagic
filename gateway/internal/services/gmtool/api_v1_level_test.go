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
	"dnf90.local/gateway/internal/modules/dnf/charstat"
	"dnf90.local/gateway/internal/modules/dnf/progression"
	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

func levelTestSource() *itemCacheTestSource {
	var xp, sp, tp strings.Builder
	for i := 1; i <= 90; i++ {
		fmt.Fprintf(&xp, "%d ", i*i*100)
		fmt.Fprintf(&sp, "%d 100\n", i)
		fmt.Fprintf(&tp, "%d 1\n", i)
	}
	return newItemCacheTestSource(readonlyTestSource{progression.ExperienceTablePath: xp.String(), progression.SkillPointTablePath: "[sp table]\n" + sp.String() + "[/sp table]\n[tp table]\n" + tp.String() + "[/tp table]\n", charstat.DefaultList: "0 `swordman.chr`\n", "character/swordman.chr": "[initial value]\n[HP MAX] 100\n[MP MAX] 50\n[strength] 8\n[fire resistance] -5\n[inventory limit] 48000\n[growtype 1]\n[HP MAX] 10\n[strength] 1\n[growtype 2]\n[HP MAX] 20\n[strength] 2\n[awakening 1]\n[HP MAX] 30\n[strength] 3\n"})
}

type levelFailStore struct {
	repository.LevelManagementRepository
}

func (s levelFailStore) SaveLevelManagement(ctx context.Context, v repository.SkillPointManagementState) error {
	if e := s.LevelManagementRepository.SaveLevelManagement(ctx, v); e != nil {
		return e
	}
	return errors.New("injected level post-save failure")
}

type levelFailUOW struct {
	repository.LevelManagementUnitOfWork
	save bool
}

func (s levelFailUOW) WithinLevelManagement(ctx context.Context, a, id string, fn func(repository.LevelManagementRepository, repository.ManagementReceiptStore) error) error {
	return s.LevelManagementUnitOfWork.WithinLevelManagement(ctx, a, id, func(r repository.LevelManagementRepository, receipts repository.ManagementReceiptStore) error {
		if s.save {
			r = levelFailStore{r}
		} else {
			receipts = failingReceipts{receipts}
		}
		return fn(r, receipts)
	})
}

func TestV1LevelMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			s, _, open := currencyFixture(t, shards)
			ctx := context.Background()
			g := open()
			source := levelTestSource()
			s.pvf = newPVFCache()
			s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
			check := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			for i, id := range []string{"10", "11", "20", "21"} {
				a := "wallet:1"
				if i > 1 {
					a = "wallet:2"
				}
				c := repository.CharacterRecord{CharacterID: id, AccountID: a, Slot: i, Name: "等级" + id, Job: "0", Level: 50, Stats: map[string]int64{"grow_type": 17, "exp": 240200, "bonus_sp": 40, "bonus_tp": 3, "gold": 777, "keep": 988, "expert_job_exp": 800, "premium_crystal_selection": -1}}
				d, e := s.levelDefinition(ctx, c, 50)
				check(e)
				for k, v := range d.Stats {
					c.Stats[k] = v
				}
				check(g.Character.Save(ctx, c))
				if id == "21" {
					continue
				}
				check(g.Skill.Save(ctx, repository.SkillRecord{CharacterID: id, Points: repository.SkillPointState{TotalSP: 5040, RemainingSP: 3000, TotalTP: 53, RemainingTP: 20, SyncedLevel: 50}, Skills: map[int64]repository.SkillState{42: {Level: 3, Enabled: true}}, Layouts: map[int]repository.SkillLayout{0: {1: 42}, 1: {2: 42}}, Cooldowns: map[int64]time.Time{42: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)}}))
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
			u := g.Character.(repository.LevelManagementUnitOfWork)
			read := func(id string) repository.SkillPointManagementState {
				v, e := open().Character.(repository.LevelManagementReader).LoadLevelManagement(ctx, id)
				check(e)
				return v
			}
			full := func(id string) (repository.CharacterRecord, repository.SkillRecord) {
				c, _, e := g.Character.Load(ctx, id)
				check(e)
				k, _, e := g.Skill.Load(ctx, id)
				check(e)
				return c, k
			}
			c0, k0 := full("10")
			oc0, ok0 := full("11")
			account0, _, e := g.Account.Load(ctx, "wallet:1")
			check(e)
			v := read("10")
			if len(v.Character.Stats) != 31 || v.Skill.Skills != nil || v.Skill.Layouts != nil || v.Skill.Cooldowns != nil {
				t.Fatal("unbounded projection", len(v.Character.Stats))
			}
			cmd := adminops.LevelCommand{AccountID: "wallet:1", CharacterID: "10", TargetLevel: 60, Revision: adminops.LevelRevision(v), PVFChecksum: source.checksum, Reason: "等级补偿", Actor: "管理员", Key: "level-same-request-001"}
			plan, e := adminops.RunLevelOperation(ctx, u, cmd, false, s.levelDefinition, nil)
			check(e)
			cmd.DefinitionRevision = plan.DefinitionRevision
			if plan.After.Experience != "348100" || plan.After.Points.TotalSP != 6040 || plan.After.Points.RemainingSP != 4000 || plan.After.Points.RemainingTP != 30 || plan.After.Stats["stat_hp_max"] <= plan.Before.Stats["stat_hp_max"] {
				t.Fatal(plan)
			}
			unchanged := func(n int) {
				t.Helper()
				c, k := full("10")
				if !reflect.DeepEqual(c, c0) || !reflect.DeepEqual(k, k0) {
					t.Fatal("failed transaction changed data")
				}
				r, e := u.LevelManagementLog(ctx, "10", 0, 100)
				check(e)
				if len(r) != n {
					t.Fatal("receipt count", len(r))
				}
			}
			unchanged(0)
			offline := func(context.Context, string) error { return nil }
			run := func(c adminops.LevelCommand) (adminops.LevelReceipt, error) {
				return adminops.RunLevelOperation(ctx, u, c, true, s.levelDefinition, offline)
			}
			for _, mode := range []string{"save", "receipt", "online", "late-online", "unavailable"} {
				worker := u
				off := offline
				switch mode {
				case "save":
					worker = levelFailUOW{u, true}
				case "receipt":
					worker = levelFailUOW{u, false}
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
				if _, e := adminops.RunLevelOperation(ctx, worker, cmd, true, s.levelDefinition, off); e == nil {
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
			if c.Level != 60 || c.Stats["exp"] != 348100 || k.Points.SyncedLevel != 60 || c.Stats["gm_level_sequence_v1"] != 1 {
				t.Fatal(c.Level, c.Stats, k.Points)
			}
			for key, want := range plan.After.Stats {
				if c.Stats[key] != want {
					t.Fatal("PVF stat persistence", key, c.Stats[key], want)
				}
			}
			if c.Stats["stat_fire_resistance"] != -5 {
				t.Fatal("negative PVF resistance lost")
			}
			keptC := repository.CloneCharacter(c)
			keptC.Level = c0.Level
			keptC.UpdatedAt = c0.UpdatedAt
			keptC.Stats["exp"] = c0.Stats["exp"]
			delete(keptC.Stats, "gm_level_sequence_v1")
			for _, key := range repository.LevelStatKeys {
				keptC.Stats[key] = c0.Stats[key]
			}
			keptK := repository.CloneSkill(k)
			keptK.Points = k0.Points
			keptK.UpdatedAt = k0.UpdatedAt
			if !reflect.DeepEqual(keptC, c0) || !reflect.DeepEqual(keptK, k0) {
				t.Fatal("unrelated state changed")
			}
			oc, ok := full("11")
			account, _, e := g.Account.Load(ctx, "wallet:1")
			check(e)
			if !reflect.DeepEqual(oc, oc0) || !reflect.DeepEqual(ok, ok0) || !reflect.DeepEqual(account0, account) {
				t.Fatal("other scope changed")
			}
			replay, e := adminops.RunLevelOperation(ctx, open().Character.(repository.LevelManagementUnitOfWork), cmd, true, nil, nil)
			check(e)
			if !reflect.DeepEqual(replay.After, plan.After) {
				t.Fatal("durable recovery")
			}
			bad := cmd
			bad.TargetLevel = 61
			if _, e = run(bad); !errors.Is(e, adminops.ErrIdempotency) {
				t.Fatal(e)
			}
			bad = cmd
			bad.Key = "level-stale-other-key"
			if _, e = run(bad); !errors.Is(e, adminops.ErrConflict) {
				t.Fatal(e)
			}
			prepare := func(id string, level int) adminops.LevelCommand {
				t.Helper()
				v := read(id)
				c := cmd
				c.AccountID = v.Character.AccountID
				c.CharacterID = id
				c.TargetLevel = level
				c.Revision = adminops.LevelRevision(v)
				c.DefinitionRevision = ""
				c.Key = fmt.Sprintf("level-test-%d", time.Now().UnixNano())
				p, e := adminops.RunLevelOperation(ctx, u, c, false, s.levelDefinition, nil)
				check(e)
				c.DefinitionRevision = p.DefinitionRevision
				return c
			}
			// Explicit downgrade preserves the spent ledger and canonical cumulative XP.
			cmd = prepare("10", 40)
			result, e := run(cmd)
			check(e)
			if result.After.Experience != "152100" || result.After.Points.RemainingSP != 2000 || result.After.Points.RemainingTP != 10 {
				t.Fatal(result)
			}
			cmd = prepare("10", 90)
			_, e = run(cmd)
			check(e)
			cmd = prepare("10", 50)
			_, e = run(cmd)
			check(e)
			bad = cmd
			bad.Revision = adminops.LevelRevision(read("10"))
			bad.TargetLevel = 1
			bad.DefinitionRevision = ""
			if _, e = adminops.RunLevelOperation(ctx, u, bad, false, s.levelDefinition, nil); !errors.Is(e, adminops.ErrSkillPointSpent) {
				t.Fatal("below spent", e)
			}
			for _, level := range []int{0, 91, -1} {
				bad.TargetLevel = level
				if _, e = adminops.RunLevelOperation(ctx, u, bad, false, s.levelDefinition, nil); !errors.Is(e, adminops.ErrLevelInput) {
					t.Fatal(level, e)
				}
			}
			bad = cmd
			bad.CharacterID = "21"
			bad.AccountID = "wallet:2"
			bad.Revision = adminops.LevelRevision(read("21"))
			if _, e = run(bad); !errors.Is(e, adminops.ErrSkillPointMissing) {
				t.Fatal("missing ledger", e)
			}
			// Skills and levels share the character lock/version boundary.
			levelCmd := prepare("20", 60)
			state := read("20")
			pd, e := s.skillPointDefinition(ctx, 50)
			check(e)
			pointCmd := adminops.SkillPointCommand{AccountID: "wallet:2", CharacterID: "20", Action: "add", Target: "sp", Amount: "10", Revision: adminops.SkillPointRevision(state), DefinitionRevision: adminops.SkillPointDefinitionRevision(pd), Reason: "并行点数", Actor: "管理员", Key: "level-vs-points-key"}
			errs = make(chan error, 2)
			go func() { _, e := run(levelCmd); errs <- e }()
			go func() {
				_, e := adminops.RunSkillPointOperation(ctx, g.Character.(repository.SkillPointManagementUnitOfWork), pointCmd, true, s.skillPointDefinition, offline)
				errs <- e
			}()
			success := 0
			for range 2 {
				e := <-errs
				if e == nil {
					success++
				} else if !errors.Is(e, adminops.ErrConflict) {
					t.Fatal(e)
				}
			}
			if success != 1 {
				t.Fatal("cross-operation serialization", success)
			}
			// Real game progression transaction locks character before skill and wins a stale plan.
			cmd = prepare("10", 60)
			locked := make(chan struct{})
			release := make(chan struct{})
			gameDone := make(chan error, 1)
			go func() {
				gameDone <- g.CharacterProgression.WithinCharacterProgression(ctx, "10", func(chars repository.CharacterRepository, skills repository.SkillRepository) error {
					c, _, e := chars.Load(ctx, "10")
					if e != nil {
						return e
					}
					if _, _, e = skills.Load(ctx, "10"); e != nil {
						return e
					}
					close(locked)
					<-release
					c.Stats["exp"]++
					return repository.SaveCharacterFields(ctx, chars, c, repository.CharacterFieldStats)
				})
			}()
			select {
			case <-locked:
			case e := <-gameDone:
				t.Fatal(e)
			case <-time.After(5 * time.Second):
				t.Fatal("game lock timeout")
			}
			done := make(chan error, 1)
			go func() { _, e := run(cmd); done <- e }()
			select {
			case e := <-done:
				close(release)
				t.Fatal("did not wait", e)
			case <-time.After(100 * time.Millisecond):
			}
			close(release)
			check(<-gameDone)
			if e = <-done; !errors.Is(e, adminops.ErrConflict) {
				t.Fatal("game conflict", e)
			}
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(adminapi.Response{Version: 1, OK: true})
			}))
			defer runtime.Close()
			s.deps.Runtime, e = adminapi.NewClient(runtime.URL+adminapi.Path, strings.Repeat("x", 32))
			check(e)
			token, e := s.startSession()
			check(e)
			status, body := v1TestCall(t, s, token, "GET", "/api/v1/characters/10/level")
			if status != 200 {
				t.Fatal(status, body)
			}
			data := body["data"].(map[string]any)
			payload := map[string]any{"accountId": "wallet:1", "targetLevel": 60, "revision": data["revision"], "pvfChecksum": source.checksum, "reason": "HTTP等级补偿"}
			call := func(path, key string, headers map[string]string) (int, map[string]any) {
				raw, _ := json.Marshal(payload)
				return operationCall(s, token, path, string(raw), key, headers)
			}
			path := "characters/10/level-operations"
			status, _ = call(path+"/preview", "", map[string]string{"X-CSRF-Token": ""})
			if status != 403 {
				t.Fatal(status)
			}
			status, body = call(path+"/preview", "", nil)
			if status != 200 {
				t.Fatal(status, body)
			}
			payload["planToken"] = body["data"].(map[string]any)["planToken"]
			payload["definitionRevision"] = body["data"].(map[string]any)["definitionRevision"]
			validToken := payload["planToken"]
			payload["targetLevel"] = 61
			status, _ = call(path, "level-http-request-001", nil)
			if status != 409 {
				t.Fatal("tampered plan", status)
			}
			payload["targetLevel"] = 60
			identity := adminops.LevelCommand{AccountID: "wallet:1", CharacterID: "10", TargetLevel: 60, Revision: payload["revision"].(string), PVFChecksum: source.checksum, DefinitionRevision: payload["definitionRevision"].(string), Reason: "HTTP等级补偿", Actor: "管理员"}
			for _, at := range []time.Time{time.Now().Add(-11 * time.Minute), time.Now().Add(time.Minute)} {
				payload["planToken"] = s.v1CursorToken("level-plan", adminops.LevelIdentity(identity), fmt.Sprint(at.Unix()))
				status, _ = call(path, "level-http-request-001", nil)
				if status != 409 {
					t.Fatal("expiry", status)
				}
			}
			payload["planToken"] = validToken
			hash := source.checksum
			source.checksum = strings.Repeat("a", 64)
			status, _ = call(path, "level-http-request-001", nil)
			if status != 409 {
				t.Fatal("PVF change", status)
			}
			source.checksum = hash
			status, body = call(path, "level-http-request-001", nil)
			if status != 200 {
				t.Fatal(status, body)
			}
			receipt := body["data"]
			s.deps.Runtime = nil
			s.deps.PVF = nil
			status, body = call(path, "level-http-request-001", nil)
			if status != 200 || !reflect.DeepEqual(receipt, body["data"]) {
				t.Fatal("offline replay", status, body)
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
			status, _ = v1TestCall(t, s, token, "GET", "/api/v1/characters/11/level-operations?limit=1&cursor="+cursor)
			if status != 400 {
				t.Fatal("cursor", status)
			}
			// Existing legacy shadows remain aligned; default mode ignores them.
			execShards("INSERT INTO DB.dnf_character_stats(character_id,stat_key,stat_value) SELECT character_id,'exp',42 FROM DB.dnf_characters WHERE character_id='11'")
			execShards("INSERT INTO DB.dnf_character_stats(character_id,stat_key,stat_value) SELECT character_id,'stat_strength',7 FROM DB.dnf_characters WHERE character_id='11'")
			if read("11").Character.Stats["exp"] != 240200 {
				t.Fatal("shadow won")
			}
			s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
			t.Run("legacy-mirror", func(t *testing.T) {
				t.Setenv("DNFBRIDGE_CHARACTER_MIRROR_STATS_AUTHORITATIVE", "off")
				v := read("11")
				if v.Character.Stats["exp"] != 42 || v.Character.Stats["stat_strength"] != 7 {
					t.Fatal(v.Character.Stats)
				}
				c := prepare("11", 55)
				r, e := run(c)
				check(e)
				fullC, _ := full("11")
				if fullC.Stats["exp"] != 291600 || !reflect.DeepEqual(fullC.Stats["stat_strength"], r.After.Stats["stat_strength"]) {
					t.Fatal("legacy update")
				}
			})
			if read("11").Character.Stats["exp"] != 291600 {
				t.Fatal("column not updated")
			}
			execShards("UPDATE DB.dnf_characters SET exp=-1 WHERE character_id='20'")
			if _, e := adminops.LevelValues(read("20")); !errors.Is(e, adminops.ErrLevelState) {
				t.Fatal("negative exp", e)
			}
		})
	}
}

func TestV1LevelDefinitionBoundedCache(t *testing.T) {
	src := levelTestSource()
	s := &Server{pvf: newPVFCache(), deps: Deps{PVF: func() (dnfpvf.Source, error) { return src, nil }}}
	c := repository.CharacterRecord{Job: "0", Stats: map[string]int64{"grow_type": 17}}
	for _, level := range []int{1, 40, 60, 90} {
		d, e := s.levelDefinition(context.Background(), c, level)
		if e != nil || d.MinimumExperience != uint32((level-1)*(level-1)*100) {
			t.Fatal(level, d, e)
		}
	}
	if src.reads != 4 {
		t.Fatal("unbounded or repeated reads", src.reads)
	}
	src.checksum = strings.Repeat("a", 64)
	d, e := s.levelDefinition(context.Background(), c, 60)
	if e != nil || d.PVFChecksum != src.checksum || src.reads != 8 {
		t.Fatal("generation", d, e, src.reads)
	}
}
