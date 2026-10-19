package gmtool

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"dnf90.local/gateway/internal/modules/dnf/adminapi"
	"dnf90.local/gateway/internal/modules/dnf/adminops"
)

func writeAccountEditFixture(t *testing.T, w http.ResponseWriter, r *http.Request, busy bool) {
	t.Helper()
	var req adminapi.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Error(err)
		w.WriteHeader(400)
		return
	}
	out := adminapi.Response{Version: 1, OK: true}
	if req.Action == "account_edit_status" {
		out.AccountEdit = &adminapi.AccountEditStatus{Protocol: adminapi.AccountEditProtocol, AccountID: req.AccountID, Busy: busy}
	}
	if req.Action == "character_edit_status" {
		out.CharacterEdit = &adminapi.CharacterEditStatus{Protocol: adminapi.CharacterEditProtocol, AccountID: req.AccountID, CharacterID: req.CharacterID, Busy: busy}
	}
	_ = json.NewEncoder(w).Encode(out)
}

func TestV1AccountEditProofRequired(t *testing.T) {
	for _, tc := range []struct {
		name  string
		proof *adminapi.AccountEditStatus
		want  error
	}{
		{"old-game", nil, adminops.ErrRuntimeUnavailable},
		{"unknown-contract", &adminapi.AccountEditStatus{Protocol: "unknown", AccountID: "a"}, adminops.ErrRuntimeUnavailable},
		{"other-account", &adminapi.AccountEditStatus{Protocol: adminapi.AccountEditProtocol, AccountID: "b"}, adminops.ErrRuntimeUnavailable},
		{"pending-login", &adminapi.AccountEditStatus{Protocol: adminapi.AccountEditProtocol, AccountID: "a", Busy: true}, adminops.ErrCharacterOnline},
		{"idle", &adminapi.AccountEditStatus{Protocol: adminapi.AccountEditProtocol, AccountID: "a"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req adminapi.Request
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Action != "account_edit_status" || req.AccountID != "a" || req.CharacterID != 0 {
					t.Error("wrong account fence request")
				}
				_ = json.NewEncoder(w).Encode(adminapi.Response{Version: 1, OK: true, AccountEdit: tc.proof})
			}))
			defer remote.Close()
			client, err := adminapi.NewClient(remote.URL+adminapi.Path, strings.Repeat("x", 32))
			if err != nil {
				t.Fatal(err)
			}
			s := &Server{deps: Deps{Runtime: client}}
			if err := s.v1AccountEditCheck("a")(context.Background(), "10"); !errors.Is(err, tc.want) {
				t.Fatal(err)
			}
		})
	}
}
