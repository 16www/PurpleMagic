package gmtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/modules/dnf/adminops"
	"dnf90.local/gateway/internal/modules/dnf/expertjob"
	"dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

type expertFailStore struct {
	repository.ExpertJobManagementRepository
}

func (s expertFailStore) SaveExpertJobManagement(ctx context.Context, v repository.ExpertJobManagementState, reset bool, ids []int64) error {
	if e := s.ExpertJobManagementRepository.SaveExpertJobManagement(ctx, v, reset, ids); e != nil {
		return e
	}
	return errors.New("injected expert save failure")
}

type expertFailUOW struct {
	repository.ExpertJobManagementUnitOfWork
	save bool
}

func (s expertFailUOW) WithinExpertJobManagement(ctx context.Context, a, id string, fn func(repository.ExpertJobManagementRepository, repository.ManagementReceiptStore) error) error {
	return s.ExpertJobManagementUnitOfWork.WithinExpertJobManagement(ctx, a, id, func(r repository.ExpertJobManagementRepository, receipt repository.ManagementReceiptStore) error {
		if s.save {
			r = expertFailStore{r}
		} else {
			receipt = failingReceipts{receipt}
		}
		return fn(r, receipt)
	})
}

func TestV1ExpertJobMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			ctx := context.Background()
			s, _, open := currencyFixture(t, shards)
			g := open()
			check := func(e error) {
				t.Helper()
				if e != nil {
					t.Fatal(e)
				}
			}
			source := expertJobFixtureSource()
			summary := filepath.Join(t.TempDir(), "items.json")
			_, e := BuildExpertJobIndex(ctx, source, summary)
			check(e)
			s.pvf = newPVFCache()
			s.items = newItemIndex(summary, s, nil)
			s.deps.PVF = func() (pvf.Source, error) { return source, nil }
			var fixtureDatabase string
			check(s.db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&fixtureDatabase))
			if !strings.HasPrefix(fixtureDatabase, "gm_currency_") || !strings.HasSuffix(fixtureDatabase, "_0") {
				t.Fatal("unexpected disposable fixture database")
			}
			fixtureSchemas := make([]string, shards)
			for i := range shards {
				fixtureSchemas[i] = fmt.Sprintf("%s_%d", strings.TrimSuffix(fixtureDatabase, "_0"), i)
			}
			for i, id := range []string{"10", "11", "20", "21"} {
				a := "wallet:1"
				if i > 1 {
					a = "wallet:2"
				}
				level := 90
				if id == "21" {
					level = 1
				}
				check(g.Character.Save(ctx, repository.CharacterRecord{CharacterID: id, AccountID: a, Slot: i, Name: "副职业" + id, Job: "0", Level: level, Stats: map[string]int64{"gold": 888, "expert_job_type": 2, "expert_job_exp": 10, "expert_job_recipe_2_123": 1, "expert_job_machine_grade": 2, "expert_job_machine_endurance": 123, "expert_job_give_up_state": 2, "premium_crystal_selection": -1, "keep": 87}}))
				updated := false
				for _, schema := range fixtureSchemas {
					result, e := s.db.ExecContext(ctx, fmt.Sprintf("UPDATE `%s`.`dnf_characters` SET `premium_crystal_selection`=-1 WHERE `character_id`=?", schema), id)
					check(e)
					count, e := result.RowsAffected()
					check(e)
					updated = updated || count > 0
				}
				if !updated {
					t.Fatal("premium_crystal_selection fixture row not found", id)
				}
				check(g.Quest.Save(ctx, repository.QuestRecord{CharacterID: id, States: map[int64]repository.QuestState{1: {Status: "active", ProgressValue: 7, Extra: map[string]string{"story": "keep"}}, 3: {Status: "completed", Extra: map[string]string{"old": "receipt"}}, 4: {Status: "active", ProgressValue: 3, RewardSelectIndex: 5, Multiplier: 3, Extra: map[string]string{"target": "keep"}}}, Progress: map[int64]repository.QuestState{3: {Status: "active"}}}))
			}
			u := g.Character.(repository.ExpertJobManagementUnitOfWork)
			reader := g.Character.(repository.ExpertJobManagementReader)
			read := func(id string) repository.ExpertJobManagementState {
				v, e := reader.LoadExpertJobManagement(ctx, id, []int64{2, 3, 4, 5})
				check(e)
				return v
			}
			full := func(id string) (repository.CharacterRecord, repository.QuestRecord) {
				c, _, e := g.Character.Load(ctx, id)
				check(e)
				q, _, e := g.Quest.Load(ctx, id)
				check(e)
				return c, q
			}
			offline := func(context.Context, string) error { return nil }
			prepare := func(id string, typ int, mode, value string) adminops.ExpertJobCommand {
				v := read(id)
				c := adminops.ExpertJobCommand{AccountID: v.Character.AccountID, CharacterID: id, TargetType: typ, Mode: mode, Value: value, Revision: adminops.ExpertJobRevision(v), PVFChecksum: source.checksum, Reason: "副职业调整", Actor: "管理员", Key: fmt.Sprintf("expert-job-%s-%d", id, time.Now().UnixNano())}
				p, e := adminops.RunExpertJobOperation(ctx, u, c, false, s.expertJobDefinitions, nil)
				check(e)
				c.DefinitionRevision = p.DefinitionRevision
				return c
			}
			before, q0 := full("10")
			other, oq := full("11")
			c := prepare("10", 3, "level", "3")
			unchanged := func() {
				a, q := full("10")
				if !reflect.DeepEqual(a, before) || !reflect.DeepEqual(q, q0) {
					t.Fatal("failed transaction changed state")
				}
				log, e := u.ExpertJobManagementLog(ctx, "10", 0, 100)
				check(e)
				if len(log) != 0 {
					t.Fatal("receipt written")
				}
			}
			unchanged()
			for _, mode := range []string{"save", "receipt", "offline", "late-online"} {
				worker := u
				off := offline
				switch mode {
				case "save":
					worker = expertFailUOW{u, true}
				case "receipt":
					worker = expertFailUOW{u, false}
				case "offline":
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
				if _, e = adminops.RunExpertJobOperation(ctx, worker, c, true, s.expertJobDefinitions, off); e == nil {
					t.Fatal(mode)
				}
				unchanged()
			}
			var wg sync.WaitGroup
			errs := make(chan error, 6)
			for range 6 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, e := adminops.RunExpertJobOperation(ctx, u, c, true, s.expertJobDefinitions, offline)
					errs <- e
				}()
			}
			wg.Wait()
			close(errs)
			for e := range errs {
				check(e)
			}
			after, q := full("10")
			if after.Stats["expert_job_type"] != 3 || after.Stats["expert_job_exp"] != 133 || after.Stats["expert_job_give_up_state"] != 2 || after.Stats["gold"] != 888 || after.Stats["keep"] != 87 || after.Stats["premium_crystal_selection"] != -1 {
				t.Fatal(after)
			}
			for _, key := range []string{"expert_job_recipe_2_123", "expert_job_machine_grade", "expert_job_machine_endurance"} {
				if _, ok := after.Stats[key]; ok {
					t.Fatal("old state retained", key)
				}
			}
			if !reflect.DeepEqual(q.States[1], q0.States[1]) || q.States[4].Status != "completed" || q.States[4].Extra["target"] != "keep" || q.States[4].RewardSelectIndex != 5 || q.States[4].Multiplier != 3 {
				t.Fatal(q)
			}
			if _, ok := q.States[3]; ok {
				t.Fatal("old branch remains")
			}
			if _, ok := q.Progress[3]; ok {
				t.Fatal("old progress remains")
			}
			a, qq := full("11")
			if !reflect.DeepEqual(a, other) || !reflect.DeepEqual(qq, oq) {
				t.Fatal("another character changed")
			}
			log, e := u.ExpertJobManagementLog(ctx, "10", 0, 100)
			check(e)
			if len(log) != 1 {
				t.Fatal("duplicate receipt")
			}
			bad := c
			bad.Value = "2"
			if _, e = adminops.RunExpertJobOperation(ctx, u, bad, true, s.expertJobDefinitions, offline); !errors.Is(e, adminops.ErrIdempotency) {
				t.Fatal(e)
			}
			// Pure experience changes preserve recipe, machine, all quest fields and extras.
			exp := prepare("11", 2, "experience", "20")
			_, e = adminops.RunExpertJobOperation(ctx, u, exp, true, s.expertJobDefinitions, offline)
			check(e)
			a, qq = full("11")
			if a.Stats["expert_job_recipe_2_123"] != 1 || a.Stats["expert_job_machine_endurance"] != 123 || !reflect.DeepEqual(qq, oq) {
				t.Fatal(a, qq)
			}
			// Replay does not need PVF or runtime availability.
			_, found, e := adminops.LookupExpertJobOperation(ctx, u, exp)
			check(e)
			if !found {
				t.Fatal("missing receipt")
			}
			stale := exp
			stale.Key = "expert-job-stale-request"
			if _, e = adminops.RunExpertJobOperation(ctx, u, stale, true, s.expertJobDefinitions, offline); !errors.Is(e, adminops.ErrConflict) {
				t.Fatal(e)
			}
			low := exp
			low.CharacterID = "21"
			low.AccountID = "wallet:2"
			low.TargetType = 3
			low.Revision = adminops.ExpertJobRevision(read("21"))
			low.DefinitionRevision = ""
			if _, e = adminops.RunExpertJobOperation(ctx, u, low, false, s.expertJobDefinitions, nil); !errors.Is(e, expertjob.ErrInput) {
				t.Fatal("level gate", e)
			}
			clear := prepare("10", 0, "experience", "0")
			_, e = adminops.RunExpertJobOperation(ctx, u, clear, true, s.expertJobDefinitions, offline)
			check(e)
			a, qq = full("10")
			if a.Stats["expert_job_type"] != 0 || a.Stats["expert_job_exp"] != 0 || len(qq.States) != 1 || len(qq.Progress) != 0 {
				t.Fatal(a, qq)
			}
			// HTTP signed preview, tamper rejection, source generation and durable recovery.
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeAccountEditFixture(t, w, r, false)
			}))
			defer runtime.Close()
			s.deps.Runtime, e = adminapi.NewClient(runtime.URL+adminapi.Path, strings.Repeat("x", 32))
			check(e)
			token, e := s.startSession()
			check(e)
			status, body := v1TestCall(t, s, token, "GET", "/api/v1/characters/10/expert-job")
			if status != 200 {
				t.Fatal(status, body)
			}
			payload := map[string]any{"accountId": "wallet:1", "targetType": 1, "mode": "level", "value": "2", "revision": body["data"].(map[string]any)["revision"], "pvfChecksum": source.checksum, "reason": "HTTP副职业"}
			call := func(path, key string, headers map[string]string) (int, map[string]any) {
				raw, _ := json.Marshal(payload)
				return operationCall(s, token, path, string(raw), key, headers)
			}
			path := "characters/10/expert-job-operations"
			status, _ = call(path+"/preview", "", map[string]string{"X-CSRF-Token": ""})
			if status != 403 {
				t.Fatal(status)
			}
			status, body = call(path+"/preview", "", nil)
			if status != 200 {
				t.Fatal(status, body)
			}
			data := body["data"].(map[string]any)
			payload["planToken"] = data["planToken"]
			payload["definitionRevision"] = data["definitionRevision"]
			payload["value"] = "3"
			status, _ = call(path, "expert-http-operation-001", nil)
			if status != 409 {
				t.Fatal("tamper", status)
			}
			payload["value"] = "2"
			hash := source.checksum
			source.checksum = strings.Repeat("b", 64)
			status, _ = call(path, "expert-http-operation-001", nil)
			if status == 200 {
				t.Fatal("new PVF accepted")
			}
			source.checksum = hash
			status, body = call(path, "expert-http-operation-001", nil)
			if status != 200 {
				t.Fatal(status, body)
			}
			receipt := body["data"]
			s.deps.PVF = nil
			s.deps.Runtime = nil
			status, body = call(path, "expert-http-operation-001", nil)
			if status != 200 || !reflect.DeepEqual(receipt, body["data"]) {
				t.Fatal("recovery", status, body)
			}
			status, body = v1TestCall(t, s, token, "GET", "/api/v1/"+path+"?limit=1")
			if status != 200 {
				t.Fatal(status, body)
			}
			cursor := url.QueryEscape(body["page"].(map[string]any)["nextCursor"].(string))
			status, _ = v1TestCall(t, s, token, "GET", "/api/v1/characters/11/expert-job-operations?limit=1&cursor="+cursor)
			if status != 400 {
				t.Fatal("cursor isolation", status)
			}
		})
	}
}
