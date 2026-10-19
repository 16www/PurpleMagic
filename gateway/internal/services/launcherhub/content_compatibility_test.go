package launcherhub

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestContentCompatibilitySignedProfileAndRestart(t *testing.T) {
	h, _ := fixture(t)
	before, _ := json.Marshal(h.state.Published)
	p := h.profile
	p.MinimumLauncherVersion = "0.2.15"
	p.ChannelHistory = &History{WindowsCodePage: 936, AccountSectionBase64: "YWNjb3VudA==", Adapter: "dnf90-history-v1"}
	if err := p.validateDirectory(); err != nil {
		t.Fatal(err)
	}
	configured, _ := json.Marshal(p)
	if err := os.WriteFile(h.cfg.ProfileFile, configured, 0600); err != nil {
		t.Fatal(err)
	}
	h, err := New(h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	still, _ := json.Marshal(h.state.Published)
	if string(before) != string(still) {
		t.Fatal("restart changed existing signed payload")
	}
	h.state.Published.Profile = h.profile
	h.state.Published.Sequence++
	if err = h.save(); err != nil {
		t.Fatal(err)
	}
	h, err = New(h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	w := call(h.PublicHandler(), "GET", "/launcher/v2/bootstrap.json", "", nil, "", "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	var envelope Envelope
	if err = json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(envelope.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var published Bootstrap
	if err = json.Unmarshal(raw, &published); err != nil {
		t.Fatal(err)
	}
	if published.Sequence != 2 || published.Profile.ChannelHistory.Adapter != "dnf90-history-v1" || published.Profile.MinimumLauncherVersion != "0.2.15" {
		t.Fatal("signed compatibility metadata lost on restart")
	}
	old, _ := json.Marshal(Profile{})
	if strings.Contains(string(old), "MinimumLauncherVersion") || strings.Contains(string(old), "Adapter") {
		t.Fatal("legacy profile representation changed")
	}
	for _, minimum := range []string{"latest", "1.2", "-1.2.3", "999999.2.3"} {
		p := Profile{MinimumLauncherVersion: minimum}
		if p.validateDirectory() == nil {
			t.Fatal("invalid minimum", minimum)
		}
	}
}
