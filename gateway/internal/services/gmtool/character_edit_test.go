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

func TestV1CharacterEditProofRequired(t *testing.T) {
	for _, tc := range []struct {
		name  string
		proof *adminapi.CharacterEditStatus
		want  error
	}{
		{"old-game", nil, adminops.ErrRuntimeUnavailable},
		{"wrong-protocol", &adminapi.CharacterEditStatus{Protocol: "online-only", AccountID: "a", CharacterID: 10}, adminops.ErrRuntimeUnavailable},
		{"wrong-account", &adminapi.CharacterEditStatus{Protocol: adminapi.CharacterEditProtocol, AccountID: "b", CharacterID: 10}, adminops.ErrRuntimeUnavailable},
		{"wrong-character", &adminapi.CharacterEditStatus{Protocol: adminapi.CharacterEditProtocol, AccountID: "a", CharacterID: 11}, adminops.ErrRuntimeUnavailable},
		{"target-busy", &adminapi.CharacterEditStatus{Protocol: adminapi.CharacterEditProtocol, AccountID: "a", CharacterID: 10, Busy: true}, adminops.ErrCharacterOnline},
		{"sibling-online", &adminapi.CharacterEditStatus{Protocol: adminapi.CharacterEditProtocol, AccountID: "a", CharacterID: 10}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var req adminapi.Request
				if e := json.NewDecoder(r.Body).Decode(&req); e != nil || req.Action != "character_edit_status" || req.AccountID != "a" || req.CharacterID != 10 {
					t.Error("wrong target")
				}
				json.NewEncoder(w).Encode(adminapi.Response{Version: 1, OK: true, CharacterEdit: tc.proof, AccountEdit: &adminapi.AccountEditStatus{Protocol: adminapi.AccountEditProtocol, AccountID: "a", Busy: true}})
			}))
			defer remote.Close()
			client, e := adminapi.NewClient(remote.URL+adminapi.Path, strings.Repeat("x", 32))
			if e != nil {
				t.Fatal(e)
			}
			s := &Server{deps: Deps{Runtime: client}}
			if e = s.v1CharacterEditCheck("a")(context.Background(), "10"); !errors.Is(e, tc.want) {
				t.Fatal(e)
			}
		})
	}
}
