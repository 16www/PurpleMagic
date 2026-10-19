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

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/modules/dnf/adminops"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

type containerFailingStore struct {
	repository.ContainerManagementRepository
}

func (containerFailingStore) SaveContainerManagement(context.Context, repository.ContainerManagementState, []string) error {
	return errors.New("injected container save error")
}

type containerFailingUOW struct {
	repository.ContainerManagementUnitOfWork
	save bool
}

func (f containerFailingUOW) WithinContainerManagement(ctx context.Context, a, id string, fn func(repository.ContainerManagementRepository, repository.ManagementReceiptStore) error) error {
	return f.ContainerManagementUnitOfWork.WithinContainerManagement(ctx, a, id, func(g repository.ContainerManagementRepository, r repository.ManagementReceiptStore) error {
		if f.save {
			g = containerFailingStore{g}
		} else {
			r = failingReceipts{r}
		}
		return fn(g, r)
	})
}

func TestV1ContainerOperationsMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			s, _, open := currencyFixture(t, shards)
			ctx := context.Background()
			g := open()
			for i, id := range []string{"10", "11", "20"} {
				if e := g.Character.Save(ctx, repository.CharacterRecord{CharacterID: id, AccountID: "wallet:1", Slot: i, Name: "容量" + id, Stats: map[string]int64{"gold": 777, "keep": 987}}); e != nil {
					t.Fatal(e)
				}
				if id == "20" {
					continue
				}
				if e := g.Settings.Save(ctx, repository.SettingsRecord{Scope: repository.CharacterContainerStateScope(id), Values: map[string]string{"main_list_param16": "0", "avatar_list_param16": "0", "personal_cargo_list_param16": "8", "account_cargo_selection_key": "3", "account_cargo_value32": "0x1234", "source": "game-original", "avatar_inventory_last_upgrade_commodity": "123", "unknown": strings.Repeat("x", 100000)}}); e != nil {
					t.Fatal(e)
				}
			}
			u := g.Character.(repository.ContainerManagementUnitOfWork)
			read := func(id string) repository.ContainerManagementState {
				v, e := open().Character.(repository.ContainerManagementReader).LoadContainerManagement(ctx, id)
				if e != nil {
					t.Fatal(e)
				}
				return v
			}
			settings := func(id string) repository.SettingsRecord {
				v, found, e := open().Settings.Load(ctx, repository.CharacterContainerStateScope(id))
				if e != nil || !found {
					t.Fatal(e)
				}
				return v
			}
			before := settings("10")
			other := settings("11")
			character, _, _ := g.Character.Load(ctx, "10")
			state := read("10")
			if len(state.Settings.Values) != 5 || state.Settings.Values["unknown"] != "" {
				t.Fatal("unbounded projection")
			}
			cmd := adminops.ContainerCommand{AccountID: "wallet:1", CharacterID: "10", Targets: map[string]int{"main": 8, "avatars": 7, "warehouse": 24}, Revision: adminops.ContainerRevision(state), Reason: "容量补偿", Actor: "测试管理员", Key: "container-request-001"}
			plan, e := adminops.RunContainerOperation(ctx, u, cmd, false, nil)
			if e != nil || len(plan.Changes) != 3 {
				t.Fatal(plan, e)
			}
			checkUnchanged := func(n int) {
				t.Helper()
				if !reflect.DeepEqual(before, settings("10")) {
					t.Fatal("unexpected settings write")
				}
				rows, e := u.ContainerManagementLog(ctx, "10", 0, 10)
				if e != nil || len(rows) != n {
					t.Fatal("unexpected receipt", len(rows), e)
				}
			}
			checkUnchanged(0)
			offline := func(context.Context, string) error { return nil }
			for _, mode := range []string{"receipt", "save", "online", "unknown", "late-online"} {
				worker := u
				check := offline
				switch mode {
				case "receipt":
					worker = containerFailingUOW{u, false}
				case "save":
					worker = containerFailingUOW{u, true}
				case "online":
					check = func(context.Context, string) error { return adminops.ErrCharacterOnline }
				case "unknown":
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
				if _, e := adminops.RunContainerOperation(ctx, worker, cmd, true, check); e == nil {
					t.Fatal(mode, "accepted")
				}
				checkUnchanged(0)
			}
			var wg sync.WaitGroup
			errs := make(chan error, 6)
			for range 6 {
				wg.Add(1)
				go func() { defer wg.Done(); _, e := adminops.RunContainerOperation(ctx, u, cmd, true, offline); errs <- e }()
			}
			wg.Wait()
			close(errs)
			for e := range errs {
				if e != nil {
					t.Fatal(e)
				}
			}
			after := settings("10")
			expected := repository.CloneSettings(after)
			for _, key := range []string{"main_list_param16", "avatar_list_param16", "personal_cargo_list_param16"} {
				expected.Values[key] = before.Values[key]
			}
			delete(expected.Values, repository.ContainerManagementSequence)
			expected.UpdatedAt = before.UpdatedAt
			if !reflect.DeepEqual(expected, before) || !reflect.DeepEqual(other, settings("11")) {
				t.Fatal("unrelated settings changed")
			}
			c, _, _ := g.Character.Load(ctx, "10")
			if !reflect.DeepEqual(c, character) {
				t.Fatal("character fields changed")
			}
			for _, id := range []string{"10", "11"} {
				if _, found, e := g.Inventory.Load(ctx, id); e != nil || found {
					t.Fatal("capacity operation created inventory", id, found, e)
				}
			}
			if r, e := adminops.RunContainerOperation(ctx, u, cmd, true, nil); e != nil || len(r.Changes) != 3 {
				t.Fatal("replay", r, e)
			}
			bad := cmd
			bad.Reason = "changed"
			if _, e := adminops.RunContainerOperation(ctx, u, bad, true, offline); !errors.Is(e, adminops.ErrIdempotency) {
				t.Fatal(e)
			}
			bad = cmd
			bad.Key = "container-stale-request"
			if _, e := adminops.RunContainerOperation(ctx, u, bad, true, offline); !errors.Is(e, adminops.ErrConflict) {
				t.Fatal(e)
			}
			bad = cmd
			bad.AccountID = "wallet:2"
			if _, e := adminops.RunContainerOperation(ctx, u, bad, true, offline); e == nil {
				t.Fatal("wrong owner accepted")
			}
			cmd.Revision = adminops.ContainerRevision(read("10"))
			cmd.Targets = map[string]int{"warehouse": 40}
			errs = make(chan error, 6)
			for i := range 6 {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					c := cmd
					c.Key = fmt.Sprintf("container-concurrent-%d", i)
					_, e := adminops.RunContainerOperation(ctx, u, c, true, offline)
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
			if success != 1 {
				t.Fatal(success)
			}
			cmd.Revision = adminops.ContainerRevision(read("10"))
			cmd.Targets = map[string]int{"main": 0}
			if _, e := adminops.RunContainerOperation(ctx, u, cmd, false, nil); !errors.Is(e, adminops.ErrContainerDowngrade) {
				t.Fatal(e)
			}
			for _, targets := range []map[string]int{{"main": 25}, {"warehouse": 201}, {"avatars": 8}, {"unknown": 1}, {}} {
				bad = cmd
				bad.Targets = targets
				if _, e := adminops.RunContainerOperation(ctx, u, bad, false, nil); !errors.Is(e, adminops.ErrContainerInput) {
					t.Fatal(targets, e)
				}
			}
			bad = cmd
			bad.CharacterID = "20"
			bad.Targets = map[string]int{"main": 8}
			bad.Revision = adminops.ContainerRevision(read("20"))
			if _, e := adminops.RunContainerOperation(ctx, u, bad, false, nil); !errors.Is(e, adminops.ErrContainerMissing) {
				t.Fatal(e)
			}
			if _, found, e := g.Settings.Load(ctx, repository.CharacterContainerStateScope("20")); e != nil || found {
				t.Fatal("missing state invented", e)
			}
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
			status, body := v1TestCall(t, s, token, "GET", "/api/v1/characters/10/containers")
			if status != 200 {
				t.Fatal(status, body)
			}
			payload := map[string]any{"accountId": "wallet:1", "targets": map[string]int{"main": 24, "avatars": 56, "warehouse": 200}, "revision": body["data"].(map[string]any)["revision"], "reason": "HTTP扩容"}
			call := func(path, key string, headers map[string]string) (int, map[string]any) {
				raw, _ := json.Marshal(payload)
				return operationCall(s, token, path, string(raw), key, headers)
			}
			status, _ = call("characters/10/container-operations/preview", "", map[string]string{"X-CSRF-Token": ""})
			if status != 403 {
				t.Fatal(status)
			}
			status, body = call("characters/10/container-operations/preview", "", nil)
			if status != 200 {
				t.Fatal(status, body)
			}
			payload["planToken"] = body["data"].(map[string]any)["planToken"]
			payload["reason"] = "tampered"
			status, _ = call("characters/10/container-operations", "container-http-001", nil)
			if status != 409 {
				t.Fatal(status)
			}
			payload["reason"] = "HTTP扩容"
			status, body = call("characters/10/container-operations", "container-http-001", nil)
			if status != 200 {
				t.Fatal(status, body)
			}
			result := body["data"]
			s.deps.Runtime = nil
			status, body = call("characters/10/container-operations", "container-http-001", nil)
			if status != 200 || !reflect.DeepEqual(result, body["data"]) {
				t.Fatal(status, body)
			}
			values, e := adminops.ContainerValues(read("10"))
			if e != nil || values["main"] != 24 || values["avatars"] != 56 || values["warehouse"] != 200 {
				t.Fatal(values, e)
			}
			status, body = v1TestCall(t, s, token, "GET", "/api/v1/characters/10/container-operations?limit=1")
			if status != 200 || len(body["data"].([]any)) != 1 {
				t.Fatal(status, body)
			}
			cursor := body["page"].(map[string]any)["nextCursor"].(string)
			status, _ = v1TestCall(t, s, token, "GET", "/api/v1/characters/10/container-operations?limit=1&cursor="+url.QueryEscape(cursor))
			if status != 200 {
				t.Fatal(status)
			}
			status, _ = v1TestCall(t, s, token, "GET", "/api/v1/characters/11/container-operations?limit=1&cursor="+url.QueryEscape(cursor))
			if status != 400 {
				t.Fatal(status)
			}
			status, body = v1TestCall(t, s, token, "GET", "/api/v1/characters/20/containers")
			if status != 200 || body["data"].(map[string]any)["initialized"] != false {
				t.Fatal(status, body)
			}
			invalid := settings("10")
			invalid.Values["main_list_param16"] = strings.Repeat("1", 129)
			if e := g.Settings.Save(ctx, invalid); e != nil {
				t.Fatal(e)
			}
			status, _ = v1TestCall(t, s, token, "GET", "/api/v1/characters/10/containers")
			if status != 422 {
				t.Fatal("oversized header", status)
			}
			status, body = call("characters/10/container-operations", "container-http-001", nil)
			if status != 200 || !reflect.DeepEqual(result, body["data"]) {
				t.Fatal("receipt recovery depends on current settings", status, body)
			}
		})
	}
}
