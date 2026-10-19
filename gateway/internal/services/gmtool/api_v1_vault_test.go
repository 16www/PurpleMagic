package gmtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/modules/dnf/adminops"
	dnfpvf "dnf90.local/gateway/internal/modules/dnf/pvf"
	"dnf90.local/gateway/internal/modules/dnf/repository"
)

type vaultFailStore struct {
	repository.AccountVaultRepository
}

func (s vaultFailStore) SaveAccountVault(ctx context.Context, v repository.AccountVaultState, keys []string) error {
	if e := s.AccountVaultRepository.SaveAccountVault(ctx, v, keys); e != nil {
		return e
	}
	return errors.New("injected post-save failure")
}

type vaultFailUOW struct {
	repository.AccountVaultUnitOfWork
	save bool
}

func (s vaultFailUOW) WithinAccountVault(ctx context.Context, id string, fn func(repository.AccountVaultRepository, repository.ManagementReceiptStore) error) error {
	return s.AccountVaultUnitOfWork.WithinAccountVault(ctx, id, func(g repository.AccountVaultRepository, r repository.ManagementReceiptStore) error {
		if s.save {
			g = vaultFailStore{g}
		} else {
			r = failingReceipts{r}
		}
		return fn(g, r)
	})
}
func TestV1AccountVaultMySQL(t *testing.T) {
	for _, shards := range []int{1, 2} {
		t.Run(fmt.Sprint(shards), func(t *testing.T) {
			s, _, open := currencyFixture(t, shards)
			pvfText := "[upgrade info]\n1 10000000 -1 0 0 -1 8 100000000 -1 0 0 -1 120 400000000 -1 0 0 -1 240 400000000 -1 0 0 -1\n[/upgrade info]"
			definition, parseErr := adminops.ParseVaultDefinition(pvfText, "")
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			s.deps.PVF = func() (dnfpvf.Source, error) { return readonlyTestSource{adminops.VaultDefinitionPath: pvfText}, nil }

			g := open()
			ctx := context.Background()
			for i, id := range []string{"10", "11", "12", "20"} {
				account := "wallet:1"
				if id == "20" {
					account = "wallet:2"
				}
				stats := map[string]int64{"gold": 777, "keep": 987}
				if id == "12" {
					stats["delete_flag"] = 1
				}
				if e := g.Character.Save(ctx, repository.CharacterRecord{CharacterID: id, AccountID: account, Slot: i, Name: "金库" + id, Stats: stats}); e != nil {
					t.Fatal(e)
				}
			}
			loadAccount := func(id string) repository.AccountRecord {
				v, found, e := open().Account.Load(ctx, id)
				if e != nil || !found {
					t.Fatal(found, e)
				}
				return v
			}
			a := loadAccount("wallet:1")
			a.Metadata["huge"] = strings.Repeat("x", 100000)
			a.Metadata["other_purchase_receipt"] = "preserve"
			a.HonorExp = 123
			a.RepresentAccountName = "保留账号名"
			if e := g.Account.Save(ctx, a); e != nil {
				t.Fatal(e)
			}
			if e := g.AccountInventory.Save(ctx, repository.AccountInventoryRecord{AccountID: "wallet:1", Slots: map[string]repository.ItemStack{"12:1": {ItemID: 123, Count: 5}, "0:500": {ItemID: 4002, Count: 7}}}); e != nil {
				t.Fatal(e)
			}
			inv, _, _ := g.AccountInventory.Load(ctx, "wallet:1")
			char, _, _ := g.Character.Load(ctx, "10")
			beforeAccount := loadAccount("wallet:1")
			otherAccount := loadAccount("wallet:2")
			u := g.Account.(repository.AccountVaultUnitOfWork)
			read := func(id string) repository.AccountVaultState {
				v, e := open().Account.(repository.AccountVaultReader).LoadAccountVault(ctx, id)
				if e != nil {
					t.Fatal(e)
				}
				return v
			}
			state := read("wallet:1")
			if len(state.Values) != 0 {
				t.Fatal("unbounded account metadata", state.Values)
			}
			values, e := adminops.ReadVaultValues(state)
			if e != nil || values.Created || values.Gold != "0" || values.Capacity != 0 {
				t.Fatal(values, e)
			}
			level := 8
			cmd := adminops.VaultCommand{DefinitionRevision: definition.Revision, AccountID: "wallet:1", Capacity: &level, Gold: &adminops.VaultGoldChange{Action: "add", Amount: "1000"}, Revision: adminops.VaultRevision(state), Reason: "开通和金币补偿", Actor: "测试管理员", Key: "vault-request-0001"}
			plan, e := adminops.RunVaultOperation(ctx, u, cmd, definition, false, nil)
			if e != nil || plan.After.Capacity != 8 || plan.After.Gold != "1000" || plan.Before.Created {
				t.Fatal(plan, e)
			}
			checkUnchanged := func() {
				t.Helper()
				if !reflect.DeepEqual(beforeAccount, loadAccount("wallet:1")) {
					t.Fatal("unexpected account change")
				}
				rows, e := u.AccountVaultLog(ctx, "wallet:1", 0, 10)
				if e != nil || len(rows) != 0 {
					t.Fatal(rows, e)
				}
			}
			checkUnchanged()
			offline := func(context.Context, string) error { return nil }
			for _, mode := range []string{"save", "receipt", "other-online", "late-online", "unknown"} {
				worker := u
				check := offline
				calls := 0
				switch mode {
				case "save":
					worker = vaultFailUOW{u, true}
				case "receipt":
					worker = vaultFailUOW{u, false}
				case "other-online":
					check = func(_ context.Context, id string) error {
						if id == "11" {
							return adminops.ErrCharacterOnline
						}
						return nil
					}
				case "late-online":
					check = func(context.Context, string) error {
						calls++
						if calls == 3 {
							return adminops.ErrCharacterOnline
						}
						return nil
					}
				case "unknown":
					check = nil
				}
				if _, e := adminops.RunVaultOperation(ctx, worker, cmd, definition, true, check); e == nil {
					t.Fatal(mode, "accepted")
				}
				checkUnchanged()
			}
			var wg sync.WaitGroup
			errs := make(chan error, 6)
			for range 6 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, e := adminops.RunVaultOperation(ctx, u, cmd, definition, true, offline)
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
			after := loadAccount("wallet:1")
			expected := repository.CloneAccount(after)
			for _, key := range repository.AccountVaultKeys() {
				delete(expected.Metadata, key)
			}
			expected.UpdatedAt = beforeAccount.UpdatedAt
			if !reflect.DeepEqual(beforeAccount, expected) || !reflect.DeepEqual(otherAccount, loadAccount("wallet:2")) {
				t.Fatal("unrelated account data changed")
			}
			otherInv, _, _ := g.AccountInventory.Load(ctx, "wallet:1")
			otherChar, _, _ := g.Character.Load(ctx, "10")
			if !reflect.DeepEqual(inv, otherInv) || !reflect.DeepEqual(char, otherChar) {
				t.Fatal("player items or gold changed")
			}
			rows, e := u.AccountVaultLog(ctx, "wallet:1", 0, 10)
			if e != nil || len(rows) != 1 {
				t.Fatal(len(rows), e)
			}
			bad := cmd
			bad.Reason = "different"
			if _, e := adminops.RunVaultOperation(ctx, u, bad, definition, true, offline); !errors.Is(e, adminops.ErrIdempotency) {
				t.Fatal(e)
			}
			bad = cmd
			bad.Key = "vault-new-key-old-revision"
			if _, e := adminops.RunVaultOperation(ctx, u, bad, definition, true, offline); !errors.Is(e, adminops.ErrConflict) {
				t.Fatal(e)
			}
			original, e := adminops.RunVaultOperation(ctx, u, cmd, definition, true, nil)
			if e != nil || original.After.Gold != "1000" {
				t.Fatal(original, e)
			}
			// Distinct keys on the same revision can apply only once.
			state = read("wallet:1")
			cmd.Revision = adminops.VaultRevision(state)
			cmd.Capacity = nil
			cmd.Gold = &adminops.VaultGoldChange{Action: "subtract", Amount: "1"}
			errs = make(chan error, 2)
			for i := range 2 {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					c := cmd
					c.Key = fmt.Sprintf("vault-race-request-%d", i)
					_, e := adminops.RunVaultOperation(ctx, u, c, definition, true, offline)
					errs <- e
				}(i)
			}
			wg.Wait()
			close(errs)
			passed, conflicted := 0, 0
			for e := range errs {
				if e == nil {
					passed++
				} else if errors.Is(e, adminops.ErrConflict) {
					conflicted++
				} else {
					t.Fatal(e)
				}
			}
			if passed != 1 || conflicted != 1 {
				t.Fatal(passed, conflicted)
			}
			state = read("wallet:1")
			v, e := adminops.ReadVaultValues(state)
			if e != nil || v.Gold != "999" {
				t.Fatal(v, e)
			}
			// Gold and capacity bounds, invalid canonical JSON strings, and shrink.
			cmd.Revision = adminops.VaultRevision(state)
			cmd.Key = "vault-validation-0001"
			for _, gold := range []*adminops.VaultGoldChange{{Action: "add", Amount: "0"}, {Action: "subtract", Amount: "1000"}, {Action: "set", Amount: "2147483648"}, {Action: "add", Amount: "100000000"}, {Action: "set", Amount: "01"}, {Action: "set", Amount: "-1"}, {Action: "sql", Amount: "1"}} {
				bad := cmd
				bad.Gold = gold
				if _, e := adminops.RunVaultOperation(ctx, u, bad, definition, false, nil); !errors.Is(e, adminops.ErrVaultInput) {
					t.Fatal(gold, e)
				}
			}
			for _, n := range []int{0, 2, 7, 121, 200} {
				bad := cmd
				bad.Capacity = &n
				if _, e := adminops.RunVaultOperation(ctx, u, bad, definition, false, nil); !errors.Is(e, adminops.ErrVaultInput) {
					t.Fatal(n, e)
				}
			}
			n := 1
			bad = cmd
			bad.Capacity = &n
			if _, e := adminops.RunVaultOperation(ctx, u, bad, definition, false, nil); !errors.Is(e, adminops.ErrVaultDecrease) {
				t.Fatal(e)
			}
			bad = cmd
			bad.AccountID = "wallet:2"
			bad.Revision = adminops.VaultRevision(read("wallet:2"))
			if _, e := adminops.RunVaultOperation(ctx, u, bad, definition, false, nil); !errors.Is(e, adminops.ErrVaultNotCreated) {
				t.Fatal(e)
			}
			// Every active role is checked before and after the write, deleted excluded.
			calls := map[string]int{}
			cmd.Gold = &adminops.VaultGoldChange{Action: "set", Amount: "100000000"}
			if _, e := adminops.RunVaultOperation(ctx, u, cmd, definition, true, func(_ context.Context, id string) error { calls[id]++; return nil }); e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(calls, map[string]int{"10": 2, "11": 2}) {
				t.Fatal(calls)
			}
			cmd.Revision = adminops.VaultRevision(read("wallet:1"))
			cmd.Key = "vault-validation-0002"
			cmd.Gold = &adminops.VaultGoldChange{Action: "set", Amount: "0"}
			if _, e := adminops.RunVaultOperation(ctx, u, cmd, definition, true, offline); e != nil {
				t.Fatal(e)
			}
			// Actual authenticated HTTP preview/commit and receipt recovery.
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
			status, body := v1TestCall(t, s, token, "GET", "/api/v1/accounts/wallet:1/vault")
			if status != 200 {
				t.Fatal(status, body)
			}
			payload := map[string]any{"gold": map[string]any{"action": "add", "amount": "5000"}, "capacity": 240, "definitionRevision": definition.Revision, "revision": body["data"].(map[string]any)["revision"], "reason": "HTTP金库变更"}
			call := func(id, path, key string, headers map[string]string) (int, map[string]any) {
				raw, _ := json.Marshal(payload)
				return operationCall(s, token, "accounts/"+id+"/vault-operations"+path, string(raw), key, headers)
			}
			status, _ = call("wallet:1", "/preview", "", map[string]string{"X-CSRF-Token": ""})
			if status != 403 {
				t.Fatal(status)
			}
			status, body = call("wallet:1", "/preview", "", nil)
			if status != 200 {
				t.Fatal(status, body)
			}
			payload["planToken"] = body["data"].(map[string]any)["planToken"]
			payload["reason"] = "tampered"
			status, _ = call("wallet:1", "", "vault-http-000001", nil)
			if status != 409 {
				t.Fatal(status)
			}
			payload["reason"] = "HTTP金库变更"
			status, _ = call("wallet:2", "", "vault-http-000001", nil)
			if status != 409 {
				t.Fatal("cross account token accepted", status)
			}

			originalText := pvfText
			pvfText = strings.ReplaceAll(pvfText, "240 400000000", "248 400000000")
			status, _ = call("wallet:1", "", "vault-http-000001", nil)
			if status != 409 {
				t.Fatal("stale PVF plan accepted", status)
			}
			checkState, e := adminops.ReadVaultValues(read("wallet:1"))
			if e != nil || checkState.Capacity != 8 || checkState.Gold != "0" {
				t.Fatal("PVF conflict wrote data", checkState, e)
			}
			pvfText = originalText
			status, body = call("wallet:1", "", "vault-http-000001", nil)
			if status != 200 {
				t.Fatal(status, body)
			}
			httpReceipt := body["data"]
			v, e = adminops.ReadVaultValues(read("wallet:1"))
			if e != nil || v.Gold != "5000" || v.Capacity != 240 {
				t.Fatal(v, e)
			}
			status, body = v1TestCall(t, s, token, "GET", "/api/v1/accounts/wallet:1/vault-operations?limit=1")
			if status != 200 || len(body["data"].([]any)) != 1 {
				t.Fatal(status, body)
			}
			cursor := body["page"].(map[string]any)["nextCursor"].(string)
			status, _ = v1TestCall(t, s, token, "GET", "/api/v1/accounts/wallet:1/vault-operations?limit=1&cursor="+url.QueryEscape(cursor))
			if status != 200 {
				t.Fatal(status)
			}
			status, _ = v1TestCall(t, s, token, "GET", "/api/v1/accounts/wallet:2/vault-operations?limit=1&cursor="+url.QueryEscape(cursor))
			if status != 400 {
				t.Fatal(status)
			}
			// Receipt namespace doesn't pollute point currency or inventory ledgers.
			currencyRows, e := g.Account.(repository.AccountManagementUnitOfWork).AccountManagementLog(ctx, "wallet:1", 0, 10)
			if e != nil || len(currencyRows) != 0 {
				t.Fatal(currencyRows, e)
			}
			inventoryRows, e := g.AccountInventory.(repository.InventoryManagementUnitOfWork).InventoryManagementLog(ctx, "wallet:1", 0, 10)
			if e != nil || len(inventoryRows) != 0 {
				t.Fatal(inventoryRows, e)
			}
			saved := loadAccount("wallet:1")
			for _, raw := range []string{"-1", "not-a-number", strings.Repeat("1", 129), strconv.FormatUint(math.MaxUint64, 10)} {
				invalid := repository.CloneAccount(saved)
				invalid.Metadata[repository.VaultGoldKey] = raw
				if e := g.Account.Save(ctx, invalid); e != nil {
					t.Fatal(e)
				}
				status, _ = v1TestCall(t, s, token, "GET", "/api/v1/accounts/wallet:1/vault")
				if status != 422 {
					t.Fatal(raw, status)
				}
			}
			s.deps.Runtime = nil
			s.deps.PVF = nil
			status, body = call("wallet:1", "", "vault-http-000001", nil)
			if status != 200 || !reflect.DeepEqual(httpReceipt, body["data"]) {
				t.Fatal("receipt depends on current balance/runtime", status, body)
			}
			if e := g.Account.Save(ctx, saved); e != nil {
				t.Fatal(e)
			}
			state = read("wallet:1")
			saved.Metadata[repository.VaultSequenceKey] = strconv.FormatInt(math.MaxInt64, 10)
			if e := g.Account.Save(ctx, saved); e != nil {
				t.Fatal(e)
			}
			bad = cmd
			bad.Revision = adminops.VaultRevision(read("wallet:1"))
			if _, e := adminops.RunVaultOperation(ctx, u, bad, definition, false, nil); !errors.Is(e, repository.ErrVaultState) {
				t.Fatal(e)
			}
		})
	}
}
