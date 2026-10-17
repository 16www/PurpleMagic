package gmtool

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/modules/dnf/repository"
	"dnf90.local/gateway/internal/modules/dnf/testbenefits"
)

type onlineSummaryOnlyStore struct {
	repository.CharacterRepository
	repository.CharacterBatchSummaryReader
	batches *atomic.Int32
}

func (s onlineSummaryOnlyStore) Load(context.Context, string) (repository.CharacterRecord, bool, error) {
	return repository.CharacterRecord{}, false, errors.New("online selection loaded aggregate")
}
func (s onlineSummaryOnlyStore) ListByAccount(context.Context, string, int) ([]repository.CharacterRecord, error) {
	return nil, errors.New("online selection scanned accounts")
}
func (s onlineSummaryOnlyStore) SummariesForCharacters(ctx context.Context, ids []string) ([]repository.CharacterSummary, error) {
	s.batches.Add(1)
	return s.CharacterBatchSummaryReader.SummariesForCharacters(ctx, ids)
}

func TestV1OnlineMailMySQL(t *testing.T) {
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
			store, e := testbenefits.NewStore(s.db, "web_accounts")
			check(e)
			check(store.EnsureSchema(ctx))
			s.deps.Benefits = store
			members := []adminapi.OnlineMember{}
			for i := 10; i < 131; i++ {
				check(g.Character.Save(ctx, repository.CharacterRecord{CharacterID: fmt.Sprint(i), AccountID: fmt.Sprintf("wallet:%d", i%2+1), Name: fmt.Sprintf("在线%d", i), Slot: i / 2, Job: "0", Level: 90, CreatedAt: time.Now().UTC().Add(-time.Hour)}))
				if i < 130 {
					members = append(members, adminapi.OnlineMember{CharacterID: uint16(i), Generation: uint64(i + 1)})
				}
			}
			reader := g.Character.(repository.CharacterBatchSummaryReader)
			for _, ids := range [][]string{{"10", "10"}, {"01"}, {"0"}, {"65536"}} {
				if _, e = reader.SummariesForCharacters(ctx, ids); e == nil {
					t.Fatal("invalid ID batch", ids)
				}
			}
			rows, e := reader.SummariesForCharacters(ctx, []string{"11", "10", "65535"})
			check(e)
			if len(rows) != 2 || rows[0].CharacterID != "10" {
				t.Fatal(rows)
			}
			var calls, batches atomic.Int32
			response := adminapi.Response{Version: 1, OK: true, OnlineSnapshot: &adminapi.OnlineSnapshot{ID: strings.Repeat("a", 32), ObservedAt: time.Now().UTC(), Complete: true, Members: members}}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var req adminapi.Request
				if json.NewDecoder(r.Body).Decode(&req) != nil || req.Action != "online_snapshot" || r.Header.Get("Authorization") != "Bearer "+strings.Repeat("x", 32) {
					t.Error("runtime request")
					w.WriteHeader(400)
					return
				}
				json.NewEncoder(w).Encode(response)
			}))
			defer server.Close()
			s.deps.Runtime, e = adminapi.NewClient(server.URL+adminapi.Path, strings.Repeat("x", 32))
			check(e)
			wrapped := g
			wrapped.Character = onlineSummaryOnlyStore{g.Character, reader, &batches}
			s.deps.Repos = func() (repository.Group, bool) { return wrapped, true }
			token, e := s.startSession()
			check(e)
			body := `{"title":"在线活动","body":"参与奖励","gold":100}`
			post := func(path, body, key string, want int) map[string]any {
				t.Helper()
				status, out := operationCall(s, token, path, body, key, nil)
				if status != want {
					t.Fatal(path, status, out)
				}
				return out
			}
			// A client-supplied subset is not an all-online snapshot.
			post("mail/online-preview", `{"title":"活动","gold":100,"recipients":[]}`, "online-extra-fields", 400)
			post("mail/online-preview", `{"title":"活动","gold":100,"accountId":"wallet:1"}`, "online-extra-target", 400)
			response.OnlineSnapshot.Complete = false
			post("mail/online-preview", body, "online-incomplete", 503)
			response.OnlineSnapshot.Complete = true
			response.OnlineSnapshot.Members = []adminapi.OnlineMember{}
			post("mail/online-preview", body, "online-empty-request", 422)
			response.OnlineSnapshot.Members = []adminapi.OnlineMember{{CharacterID: 65535, Generation: 1}}
			post("mail/online-preview", body, "online-missing-player", 422)
			jobs, _, e := store.JobPage(ctx, "", 100)
			check(e)
			if len(jobs) != 0 {
				t.Fatal("failed snapshot created a task")
			}
			response.OnlineSnapshot.Members = members
			calls.Store(0)
			batches.Store(0)
			var wg sync.WaitGroup
			responses := make(chan map[string]any, 4)
			statuses := make(chan int, 4)
			for range 4 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					code, out := operationCall(s, token, "mail/online-preview", body, "online-fixed-request", nil)
					statuses <- code
					responses <- out
				}()
			}
			wg.Wait()
			close(statuses)
			close(responses)
			for code := range statuses {
				if code != 200 {
					t.Fatal(code)
				}
			}
			var first map[string]any
			for out := range responses {
				if first == nil {
					first = out["data"].(map[string]any)
				} else if !reflect.DeepEqual(first, out["data"]) {
					t.Fatal("concurrent snapshot changed")
				}
			}
			if calls.Load() != 1 || batches.Load() != 2 {
				t.Fatal("not bounded or resampled", calls.Load(), batches.Load())
			}
			id := first["id"].(string)
			if first["total"] != float64(120) || first["recipientSource"].(map[string]any)["count"] != float64(120) {
				t.Fatal(first)
			}
			job, e := store.Job(ctx, id)
			check(e)
			if len(job.Recipients) != 120 || job.Recipients[0].SessionGeneration == "" {
				t.Fatal("missing fixed session evidence")
			}
			box, _, e := g.Mailbox.Load(ctx, "10")
			check(e)
			if len(box.Mails) != 0 {
				t.Fatal("preview delivered")
			}
			// Runtime outage and a new Store instance do not stop recovery.
			s.deps.Runtime = nil
			store, e = testbenefits.NewStore(s.db, "web_accounts")
			check(e)
			s.deps.Benefits = store
			recovered := post("mail/online-preview", body, "online-fixed-request", 200)["data"]
			if !reflect.DeepEqual(first, recovered) {
				t.Fatal("lost preview response")
			}
			post("mail/online-preview", strings.Replace(body, "100", "101", 1), "online-fixed-request", 409)
			post("mail/online-preview", body, "online-offline-new-key", 503)
			// Delivery uses the frozen members after they go offline; new logins stay out.
			s.deps.Repos = func() (repository.Group, bool) { return open(), true }
			for revision := 0; revision < 12; revision++ {
				post("tasks/"+id+"/run", fmt.Sprintf(`{"revision":%d,"confirm":true}`, revision), "", 200)
			}
			post("tasks/"+id+"/run", `{"revision":0,"confirm":true}`, "", 409)
			for i := 10; i < 131; i++ {
				box, _, e := g.Mailbox.Load(ctx, fmt.Sprint(i))
				check(e)
				want := 1
				if i == 130 {
					want = 0
				}
				if len(box.Mails) != want {
					t.Fatal("recipient drift", i, len(box.Mails))
				}
			}
			if calls.Load() != 1 {
				t.Fatal("delivery queried runtime again")
			}
		})
	}
}
