package gmtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/modules/dnf/adminops"
	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/quest"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

type questFailingUOW struct {
	repository.CharacterManagementUnitOfWork
}

func (f questFailingUOW) WithinCharacterManagement(ctx context.Context, a, c string, fn func(repository.Group, repository.ManagementReceiptStore) error) error {
	return f.CharacterManagementUnitOfWork.WithinCharacterManagement(ctx, a, c, func(g repository.Group, r repository.ManagementReceiptStore) error { return fn(g, failingReceipts{r}) })
}

func TestV1QuestOperationsMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			s, _, open := currencyFixture(t, shards)
			ctx := context.Background()
			g := open()
			character := repository.CharacterRecord{CharacterID: "10", AccountID: "wallet:1", Name: "任务测试", Level: 90, Job: "0", Stats: map[string]int64{"grow_type": 0, "gold": 10000}}
			if err := g.Character.Save(ctx, character); err != nil {
				t.Fatal(err)
			}
			u := g.Character.(repository.CharacterManagementUnitOfWork)
			catalog, err := quest.CatalogFromDefinitions([]quest.Definition{{ID: 1, Path: "n_quest/one.qst", Type: "[meet npc]", ExposedByNPC: 1, LevelMin: 1, LevelMax: 100}, {ID: 2, Path: "n_quest/two.qst", Type: "[meet npc]", ExposedByNPC: 1, LevelMin: 1, LevelMax: 100}, {ID: 3, Path: "n_quest/three.qst", HasDependGiveItem: true}, {ID: 4, Path: "n_quest/four.qst", CantGiveUp: true}, {ID: 5, Path: "n_quest/five.qst", RewardType: "[grow type]"}})
			if err != nil {
				t.Fatal(err)
			}
			load := func() repository.QuestRecord {
				q, _, e := open().Quest.Load(ctx, "10")
				if e != nil {
					t.Fatal(e)
				}
				return q
			}
			cmd := adminops.QuestCommand{AccountID: "wallet:1", CharacterID: "10", Action: "accept", QuestIDs: []int64{1, 2}, PVFChecksum: strings.Repeat("a", 64), Reason: "任务修复", Actor: "测试管理员"}
			preview, e := adminops.RunQuestOperation(ctx, u, catalog, cmd, false, nil)
			if e != nil {
				t.Fatal(e)
			}
			if _, found, e := g.Quest.Load(ctx, "10"); e != nil || found {
				t.Fatal("preview persisted", found, e)
			}
			cmd.Revision = preview.Revision
			cmd.Key = "quest-accept-same-key"
			offline := func(context.Context, string) error { return nil }
			var wg sync.WaitGroup
			errs := make(chan error, 6)
			for range 6 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					r, e := adminops.RunQuestOperation(ctx, u, catalog, cmd, true, offline)
					if e == nil && len(r.Changes) != 2 {
						e = errors.New("missing per-quest result")
					}
					errs <- e
				}()
			}
			wg.Wait()
			close(errs)
			for e := range errs {
				if e != nil {
					t.Fatal(e)
				}
			}
			if q := load(); len(q.States) != 2 || q.States[1].Status != "active" {
				t.Fatal(q)
			}
			if rows, e := u.CharacterManagementLog(ctx, "10", 0, 10); e != nil || len(rows) != 1 {
				t.Fatal(len(rows), e)
			}
			if _, e = adminops.RunQuestOperation(ctx, open().Character.(repository.CharacterManagementUnitOfWork), catalog, cmd, true, func(context.Context, string) error { return adminops.ErrCharacterOnline }); e != nil {
				t.Fatal("durable replay should not need offline probe", e)
			}
			for _, action := range []string{"give-up", "accept"} {
				operation := cmd
				operation.Action, operation.QuestIDs = action, []int64{2}
				operation.Key = "quest-roundtrip-" + action
				plan, err := adminops.RunQuestOperation(ctx, u, catalog, operation, false, nil)
				if err != nil {
					t.Fatal(err)
				}
				operation.Revision = plan.Revision
				if _, err = adminops.RunQuestOperation(ctx, u, catalog, operation, true, offline); err != nil {
					t.Fatal(err)
				}
				_, present := load().States[2]
				if present != (action == "accept") {
					t.Fatal("give-up/accept state", action)
				}
			}
			cmd.Action = "ready"
			cmd.QuestIDs = []int64{1}
			cmd.Key = "quest-ready-request-01"
			preview, e = adminops.RunQuestOperation(ctx, u, catalog, cmd, false, nil)
			if e != nil {
				t.Fatal(e)
			}
			cmd.Revision = preview.Revision
			before := load()
			if _, e = adminops.RunQuestOperation(ctx, u, catalog, cmd, true, func(context.Context, string) error { return adminops.ErrCharacterOnline }); !errors.Is(e, adminops.ErrCharacterOnline) {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(before, load()) {
				t.Fatal("online changed data")
			}
			if _, e = adminops.RunQuestOperation(ctx, questFailingUOW{u}, catalog, cmd, true, offline); e == nil {
				t.Fatal("receipt failure accepted")
			}
			if !reflect.DeepEqual(before, load()) {
				t.Fatal("rollback lost original quest")
			}
			var calls atomic.Int32
			if _, e = adminops.RunQuestOperation(ctx, u, catalog, cmd, true, func(context.Context, string) error {
				if calls.Add(1) == 2 {
					return adminops.ErrCharacterOnline
				}
				return nil
			}); !errors.Is(e, adminops.ErrCharacterOnline) {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(before, load()) {
				t.Fatal("late online check failed to rollback")
			}
			if _, e = adminops.RunQuestOperation(ctx, u, catalog, cmd, true, offline); e != nil {
				t.Fatal(e)
			}
			if q := load(); q.States[1].Extra["gm_marked_ready"] != "true" {
				t.Fatal(q)
			}
			cmd.Action = "complete"
			cmd.QuestIDs = []int64{1, 5}
			cmd.Key = "quest-atomic-batch-01"
			if _, e = adminops.RunQuestOperation(ctx, u, catalog, cmd, false, nil); !errors.Is(e, quest.ErrAdministrationProfession) {
				t.Fatal(e)
			}
			cmd.QuestIDs = []int64{1, 2}
			preview, e = adminops.RunQuestOperation(ctx, u, catalog, cmd, false, nil)
			if e != nil {
				t.Fatal(e)
			}
			cmd.Revision = preview.Revision
			result, e := adminops.RunQuestOperation(ctx, u, catalog, cmd, true, offline)
			if e != nil || result.RewardGranted || len(result.Changes) != 2 {
				t.Fatal(result, e)
			}
			q := load()
			for _, id := range []int64{1, 2} {
				if q.States[id].Status != "completed" || q.States[id].Extra["reward_state"] != "granted" || q.States[id].Extra["gm_rewards_granted"] != "false" {
					t.Fatal(q)
				}
			}
			if c, _, e := g.Character.Load(ctx, "10"); e != nil || c.Stats["gold"] != 10000 {
				t.Fatal("completion changed gold", c, e)
			}
			cmd.Key = "quest-stale-version-1"
			if _, e = adminops.RunQuestOperation(ctx, u, catalog, cmd, true, offline); !errors.Is(e, adminops.ErrConflict) {
				t.Fatal(e)
			}
			// Different request IDs based on one revision cannot both commit.
			raceCommand := cmd
			raceCommand.Action = "reset"
			raceCommand.QuestIDs = []int64{2}
			raceCommand.Revision = result.Revision
			outcomes := make(chan error, 2)
			for _, key := range []string{"quest-race-request-01", "quest-race-request-02"} {
				copy := raceCommand
				copy.Key = key
				go func() { _, err := adminops.RunQuestOperation(ctx, u, catalog, copy, true, offline); outcomes <- err }()
			}
			wins, conflicts := 0, 0
			for range 2 {
				e := <-outcomes
				if e == nil {
					wins++
				} else if errors.Is(e, adminops.ErrConflict) {
					conflicts++
				} else {
					t.Fatal(e)
				}
			}
			if wins != 1 || conflicts != 1 {
				t.Fatal(wins, conflicts)
			}
			cmd.Action = "reset"
			cmd.QuestIDs = []int64{1}
			preview, e = adminops.RunQuestOperation(ctx, u, catalog, cmd, false, nil)
			if e != nil {
				t.Fatal(e)
			}
			cmd.Revision = preview.Revision
			cmd.Key = "quest-reset-request-1"
			if _, e = adminops.RunQuestOperation(ctx, u, catalog, cmd, true, offline); e != nil {
				t.Fatal(e)
			}
			if _, ok := load().States[1]; ok {
				t.Fatal("reset left quest row")
			}
			cmd.Action = "accept"
			cmd.QuestIDs = []int64{1, 3}
			if _, e = adminops.RunQuestOperation(ctx, u, catalog, cmd, false, nil); !errors.Is(e, quest.ErrQuestAcceptEventItemsRequired) {
				t.Fatal(e)
			}
			if _, ok := load().States[1]; ok {
				t.Fatal("failed preview changed quest")
			}
			cmd.CharacterID = "0010"
			if _, e = adminops.RunQuestOperation(ctx, u, catalog, cmd, false, nil); !errors.Is(e, adminops.ErrQuestInput) {
				t.Fatal("alias accepted", e)
			}
			cmd.CharacterID = "10"
			cmd.AccountID = "wallet:2"
			if _, e = adminops.RunQuestOperation(ctx, u, catalog, cmd, false, nil); e == nil {
				t.Fatal("wrong owner accepted")
			}
			// Exercise the real HTTP adapters using indexed PVF fixture definitions.
			source := questFixture("第一任务")
			path := filepath.Join(t.TempDir(), "items.json")
			if _, e = BuildQuestIndex(ctx, source, path); e != nil {
				t.Fatal(e)
			}
			s.items = newItemIndex(path, s, nil)
			s.deps.PVF = func() (dnfpvf.Source, error) { return source, nil }
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(adminapi.Response{Version: 1, OK: true, Online: false})
			}))
			defer runtime.Close()
			s.deps.Runtime, e = adminapi.NewClient(runtime.URL+adminapi.Path, strings.Repeat("x", 32))
			if e != nil {
				t.Fatal(e)
			}
			token, _ := s.startSession()
			body := map[string]any{"accountId": "wallet:1", "action": "complete", "questIds": []string{"1"}, "reason": "HTTP测试"}
			raw, _ := json.Marshal(body)
			status, out := operationCall(s, token, "characters/10/quest-operations/preview", string(raw), "", nil)
			if status != 200 {
				t.Fatal(status, out)
			}
			data := out["data"].(map[string]any)
			body["revision"] = data["revision"]
			body["pvfChecksum"] = data["pvfChecksum"]
			raw, _ = json.Marshal(body)
			status, out = operationCall(s, token, "characters/10/quest-operations", string(raw), "quest-http-request-01", nil)
			if status != 200 {
				t.Fatal(status, out)
			}
			status, out = v1TestCall(t, s, token, "GET", "/api/v1/characters/10/quest-operations?limit=1")
			if status != 200 || len(out["data"].([]any)) != 1 {
				t.Fatal(status, out)
			}
			nextCursor := out["page"].(map[string]any)["nextCursor"].(string)
			status, out = v1TestCall(t, s, token, "GET", "/api/v1/characters/10/quest-operations?limit=1&cursor="+nextCursor)
			if status != 200 || len(out["data"].([]any)) != 1 {
				t.Fatal("ledger pagination", status, out)
			}
			status, _ = v1TestCall(t, s, token, "GET", "/api/v1/characters/10/quest-operations?limit=2&cursor="+nextCursor)
			if status != 400 {
				t.Fatal("cursor filter accepted", status)
			}
			// Committed request recovery works without the current PVF/runtime.
			originalPVF, originalRuntime := s.deps.PVF, s.deps.Runtime
			s.deps.PVF = nil
			s.deps.Runtime = nil
			status, out = operationCall(s, token, "characters/10/quest-operations", string(raw), "quest-http-request-01", nil)
			if status != 200 || out["data"].(map[string]any)["requestId"] != "quest-http-request-01" {
				t.Fatal("receipt recovery", status, out)
			}
			s.deps.PVF, s.deps.Runtime = originalPVF, originalRuntime
			body["pvfChecksum"] = strings.Repeat("f", 64)
			raw, _ = json.Marshal(body)
			status, _ = operationCall(s, token, "characters/10/quest-operations", string(raw), "quest-http-request-02", nil)
			if status != 409 {
				t.Fatal("stale PVF accepted", status)
			}
		})
	}
}
