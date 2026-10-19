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
	"sync/atomic"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/modules/dnf/adminops"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

type goldFailingCharacter struct {
	repository.CharacterGoldRepository
}

func (goldFailingCharacter) SaveCharacterGold(context.Context, repository.CharacterRecord) error {
	return errors.New("injected gold save failure")
}

type goldFailingUOW struct {
	repository.CharacterGoldUnitOfWork
	save bool
}

func (f goldFailingUOW) WithinCharacterGold(ctx context.Context, a, c string, fn func(repository.CharacterGoldRepository, repository.ManagementReceiptStore) error) error {
	return f.CharacterGoldUnitOfWork.WithinCharacterGold(ctx, a, c, func(g repository.CharacterGoldRepository, r repository.ManagementReceiptStore) error {
		if f.save {
			g = goldFailingCharacter{g}
		} else {
			r = failingReceipts{r}
		}
		return fn(g, r)
	})
}

func TestV1GoldOperationsMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			s, _, open := currencyFixture(t, shards)
			ctx := context.Background()
			g := open()
			u := g.Character.(repository.CharacterGoldUnitOfWork)
			for i, id := range []string{"10", "11", "20"} {
				a := "wallet:1"
				if id == "20" {
					a = "wallet:2"
				}
				c := repository.CharacterRecord{CharacterID: id, AccountID: a, Slot: i, Name: "金币测试" + id, Job: "0", Level: 90, Stats: map[string]int64{"gold": 10000, "cera": 777, "carry_gold_stage": 2, "stat_inventory_limit": 700000, "exp": 123, "keep": 998}, Location: repository.CharacterLocation{ChannelID: 5}}
				if e := g.Character.Save(ctx, c); e != nil {
					t.Fatal(e)
				}
			}
			load := func(id string) repository.CharacterRecord {
				c, found, e := open().Character.Load(ctx, id)
				if e != nil || !found {
					t.Fatal(found, e)
				}
				return c
			}
			read := func(id string) repository.CharacterRecord {
				c, found, e := open().Character.(repository.CharacterGoldReader).LoadCharacterGold(ctx, id)
				if e != nil || !found {
					t.Fatal(found, e)
				}
				return c
			}
			original := load("10")
			other := load("11")
			account, _, _ := g.Account.Load(ctx, "wallet:1")
			if adminops.CharacterGoldRevision(original) != adminops.CharacterGoldRevision(read("10")) {
				t.Fatal("bounded projection differs")
			}
			command := adminops.GoldCommand{AccountID: "wallet:1", CharacterID: "10", Action: "add", Amount: "250", Revision: adminops.CharacterGoldRevision(original), Reason: "金币补偿", Actor: "测试管理员", Key: "gold-concurrent-same-key"}
			preview, e := adminops.RunGoldOperation(ctx, u, command, false, nil)
			if e != nil || preview.Before != "10000" || preview.After != "10250" || preview.Delta != "250" {
				t.Fatal(preview, e)
			}
			if !reflect.DeepEqual(original, load("10")) {
				t.Fatal("preview wrote character")
			}
			rows, e := u.CharacterGoldLog(ctx, "10", 0, 10)
			if e != nil || len(rows) != 0 {
				t.Fatal("preview receipt", len(rows), e)
			}
			offline := func(context.Context, string) error { return nil }
			assertUnchanged := func(before repository.CharacterRecord, n int) {
				t.Helper()
				if !reflect.DeepEqual(before, load("10")) {
					t.Fatal("failed operation changed character")
				}
				rows, e := u.CharacterGoldLog(ctx, "10", 0, 100)
				if e != nil || len(rows) != n {
					t.Fatal("failed operation wrote receipt", len(rows), e)
				}
			}
			for _, mode := range []string{"receipt", "save", "online", "unavailable", "late-online"} {
				check := offline
				worker := u
				switch mode {
				case "receipt":
					worker = goldFailingUOW{u, false}
				case "save":
					worker = goldFailingUOW{u, true}
				case "online":
					check = func(context.Context, string) error { return adminops.ErrCharacterOnline }
				case "unavailable":
					check = nil
				case "late-online":
					var calls atomic.Int32
					check = func(context.Context, string) error {
						if calls.Add(1) == 2 {
							return adminops.ErrCharacterOnline
						}
						return nil
					}
				}
				if _, e := adminops.RunGoldOperation(ctx, worker, command, true, check); e == nil {
					t.Fatal(mode, "accepted")
				}
				assertUnchanged(original, 0)
			}
			var wg sync.WaitGroup
			errs := make(chan error, 6)
			for range 6 {
				wg.Add(1)
				go func() { defer wg.Done(); _, e := adminops.RunGoldOperation(ctx, u, command, true, offline); errs <- e }()
			}
			wg.Wait()
			close(errs)
			for e := range errs {
				if e != nil {
					t.Fatal(e)
				}
			}
			changed := load("10")
			if changed.Stats["gold"] != 10250 || changed.Stats["gm_gold_sequence_v1"] != 1 {
				t.Fatal(changed.Stats)
			}
			expected := repository.CloneCharacter(changed)
			expected.Stats["gold"] = original.Stats["gold"]
			delete(expected.Stats, "gm_gold_sequence_v1")
			expected.UpdatedAt = original.UpdatedAt
			if !reflect.DeepEqual(expected, original) || !reflect.DeepEqual(other, load("11")) {
				t.Fatal("unrelated fields changed")
			}
			afterAccount, _, _ := g.Account.Load(ctx, "wallet:1")
			if !reflect.DeepEqual(account, afterAccount) {
				t.Fatal("account currency changed")
			}
			replay, e := adminops.RunGoldOperation(ctx, open().Character.(repository.CharacterGoldUnitOfWork), command, true, nil)
			if e != nil || replay.After != "10250" || replay.Revision != adminops.CharacterGoldRevision(read("10")) {
				t.Fatal("durable replay", replay, e)
			}
			rows, e = u.CharacterGoldLog(ctx, "10", 0, 10)
			if e != nil || len(rows) != 1 {
				t.Fatal(len(rows), e)
			}
			bad := command
			bad.Amount = "251"
			if _, e := adminops.RunGoldOperation(ctx, u, bad, true, offline); !errors.Is(e, adminops.ErrIdempotency) {
				t.Fatal("key conflict", e)
			}
			bad = command
			bad.Key = "gold-stale-revision"
			if _, e := adminops.RunGoldOperation(ctx, u, bad, true, offline); !errors.Is(e, adminops.ErrConflict) {
				t.Fatal("revision", e)
			}
			bad = command
			bad.AccountID = "wallet:2"
			if _, e := adminops.RunGoldOperation(ctx, u, bad, true, offline); e == nil {
				t.Fatal("wrong owner accepted")
			}
			assertUnchanged(changed, 1)
			command.Revision = adminops.CharacterGoldRevision(changed)
			command.Action = "subtract"
			command.Amount = "50"
			errs = make(chan error, 6)
			for i := range 6 {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					c := command
					c.Key = fmt.Sprintf("gold-distinct-request-%d", i)
					_, e := adminops.RunGoldOperation(ctx, u, c, true, offline)
					errs <- e
				}(i)
			}
			wg.Wait()
			close(errs)
			success := 0
			for e := range errs {
				if e == nil {
					success++
				} else if !errors.Is(e, adminops.ErrConflict) {
					t.Fatal(e)
				}
			}
			if success != 1 || load("10").Stats["gold"] != 10200 {
				t.Fatal("concurrent revision", success)
			}
			for i, n := range []string{"0", "2147483647", "10000"} {
				command.Action = "set"
				command.Amount = n
				command.Revision = adminops.CharacterGoldRevision(read("10"))
				command.Key = fmt.Sprintf("gold-set-request-%d", i)
				r, e := adminops.RunGoldOperation(ctx, u, command, true, offline)
				if e != nil || r.After != n {
					t.Fatal(r, e)
				}
			}
			if adminops.CharacterGoldRevision(read("10")) == adminops.CharacterGoldRevision(original) {
				t.Fatal("ABA revision reused")
			}
			// Reject canonical/range errors before mutation, including int64 overflow.
			for _, row := range [][2]string{{"add", "0"}, {"set", "-1"}, {"set", "01"}, {"set", "+1"}, {"set", "1.5"}, {"set", "2147483648"}, {"add", "9223372036854775807"}, {"subtract", "10001"}, {"add", "2147483647"}, {"replace", "1"}} {
				bad = command
				bad.Action = row[0]
				bad.Amount = row[1]
				bad.Revision = adminops.CharacterGoldRevision(read("10"))
				bad.Key = "gold-invalid-request-01"
				if _, e := adminops.RunGoldOperation(ctx, u, bad, true, offline); !errors.Is(e, adminops.ErrGoldInput) {
					t.Fatal(row, e)
				}
			}
			// HTTP signed previews, ownership, CSRF, readback, replay and page binding.
			runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				writeAccountEditFixture(t, w, r, false)
			}))
			defer runtime.Close()
			s.deps.Runtime, e = adminapi.NewClient(runtime.URL+adminapi.Path, strings.Repeat("x", 32))
			if e != nil {
				t.Fatal(e)
			}
			token, e := s.startSession()
			if e != nil {
				t.Fatal(e)
			}
			status, body := v1TestCall(t, s, token, "GET", "/api/v1/characters/10/gold")
			if status != 200 || body["data"].(map[string]any)["balance"] != "10000" {
				t.Fatal(status, body)
			}
			payload := map[string]any{"accountId": "wallet:1", "action": "add", "amount": "123", "revision": body["data"].(map[string]any)["revision"], "reason": "网页补偿"}
			call := func(path, key string, headers map[string]string) (int, map[string]any) {
				raw, _ := json.Marshal(payload)
				return operationCall(s, token, path, string(raw), key, headers)
			}
			status, _ = call("characters/10/gold-operations/preview", "", map[string]string{"X-CSRF-Token": ""})
			if status != 403 {
				t.Fatal("csrf", status)
			}
			status, body = call("characters/10/gold-operations/preview", "", nil)
			if status != 200 {
				t.Fatal(status, body)
			}
			plan := body["data"].(map[string]any)
			payload["planToken"] = plan["planToken"]
			payload["amount"] = "124"
			status, _ = call("characters/10/gold-operations", "gold-http-request-001", nil)
			if status != 409 {
				t.Fatal("tampered plan", status)
			}
			payload["amount"] = "123"
			validToken := payload["planToken"]
			identity := adminops.GoldCommand{AccountID: "wallet:1", CharacterID: "10", Action: "add", Amount: "123", Revision: payload["revision"].(string), Reason: "网页补偿", Actor: "管理员"}
			for _, at := range []time.Time{time.Now().Add(-11 * time.Minute), time.Now().Add(time.Minute)} {
				payload["planToken"] = s.v1CursorToken("gold-plan", adminops.GoldIdentity(identity), fmt.Sprint(at.Unix()))
				status, _ = call("characters/10/gold-operations", "gold-http-request-001", nil)
				if status != 409 {
					t.Fatal("expired/future plan", status)
				}
			}
			payload["planToken"] = validToken
			status, body = call("characters/10/gold-operations", "gold-http-request-001", nil)
			if status != 200 || body["data"].(map[string]any)["after"] != "10123" {
				t.Fatal(status, body)
			}
			committed := body["data"]
			s.deps.Runtime = nil
			status, body = call("characters/10/gold-operations", "gold-http-request-001", nil)
			if status != 200 || !reflect.DeepEqual(body["data"], committed) {
				t.Fatal("offline recovery", status, body)
			}
			status, body = v1TestCall(t, s, token, "GET", "/api/v1/characters/10/gold-operations?limit=1")
			if status != 200 || len(body["data"].([]any)) != 1 {
				t.Fatal(status, body)
			}
			cursor := body["page"].(map[string]any)["nextCursor"].(string)
			status, body = v1TestCall(t, s, token, "GET", "/api/v1/characters/10/gold-operations?limit=1&cursor="+url.QueryEscape(cursor))
			if status != 200 || len(body["data"].([]any)) != 1 {
				t.Fatal(status, body)
			}
			status, _ = v1TestCall(t, s, token, "GET", "/api/v1/characters/11/gold-operations?limit=1&cursor="+url.QueryEscape(cursor))
			if status != 400 {
				t.Fatal("cross-character cursor", status)
			}
			for _, namespace := range []string{"quest-operations", "inventory-operations"} {
				status, body = v1TestCall(t, s, token, "GET", "/api/v1/characters/10/"+namespace)
				if status != 200 || len(body["data"].([]any)) != 0 {
					t.Fatal("receipt namespace", status, body)
				}
			}
			var database string
			if e := s.db.QueryRow("SELECT DATABASE()").Scan(&database); e != nil {
				t.Fatal(e)
			}
			for shard := 0; shard < shards; shard++ {
				db := fmt.Sprintf("`%s%d`", strings.TrimSuffix(database, "0"), shard)
				if _, e := s.db.Exec("INSERT INTO " + db + ".dnf_character_stats (character_id,stat_key,stat_value) SELECT character_id,'gold',42 FROM " + db + ".dnf_characters WHERE character_id='20'"); e != nil {
					t.Fatal(e)
				}
			}
			if read("20").Stats["gold"] != 10000 {
				t.Fatal("mirror shadow overwrote authoritative gold")
			}
			t.Run("legacy-stat-compat", func(t *testing.T) {
				t.Setenv("DNFBRIDGE_CHARACTER_MIRROR_STATS_AUTHORITATIVE", "off")
				ch := read("20")
				if ch.Stats["gold"] != 42 || adminops.CharacterGoldRevision(ch) != adminops.CharacterGoldRevision(load("20")) {
					t.Fatal("legacy projection mismatch")
				}
				cmd := adminops.GoldCommand{AccountID: "wallet:2", CharacterID: "20", Action: "set", Amount: "43", Revision: adminops.CharacterGoldRevision(ch), Actor: "测试管理员", Reason: "旧配置兼容", Key: "gold-legacy-config-01"}
				if _, e := adminops.RunGoldOperation(ctx, u, cmd, true, offline); e != nil {
					t.Fatal(e)
				}
				if read("20").Stats["gold"] != 43 || load("20").Stats["gold"] != 43 {
					t.Fatal("legacy write mismatch")
				}
			})
			for _, value := range []int64{-1, 2147483648} {
				// Seed corrupt columns directly: the legacy full-record writer
				// normalizes negative stats, so it cannot create this fixture.
				for shard := 0; shard < shards; shard++ {
					table := fmt.Sprintf("`%s%d`.dnf_characters", strings.TrimSuffix(database, "0"), shard)
					if _, e := s.db.Exec("UPDATE "+table+" SET gold=? WHERE character_id='20'", value); e != nil {
						t.Fatal(e)
					}
				}
				status, _ = v1TestCall(t, s, token, "GET", "/api/v1/characters/20/gold")
				if status != 422 {
					t.Fatal("bad stored balance", status)
				}
			}
			if _, found, e := g.Character.(repository.CharacterGoldReader).LoadCharacterGold(ctx, "010"); e != nil || found {
				t.Fatal("numeric alias", found, e)
			}
			bad = command
			bad.Revision = strings.Repeat("g", 64)
			if _, e := adminops.RunGoldOperation(ctx, u, bad, false, nil); !errors.Is(e, adminops.ErrGoldInput) {
				t.Fatal(e)
			}
		})
	}
}
