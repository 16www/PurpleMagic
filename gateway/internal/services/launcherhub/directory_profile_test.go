package launcherhub

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func directoryID(value int) *int { return &value }

func TestDirectoryProfileValidation(t *testing.T) {
	for _, p := range []Profile{{}, {ChannelServerID: 1}, {ChannelServerID: 249}, {ChannelServerID: 1, PreviousChannelServerID: directoryID(0)}} {
		if err := p.validateDirectory(); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []Profile{{ChannelServerID: -1}, {ChannelServerID: 250}, {PreviousChannelServerID: directoryID(0)}, {PreviousChannelServerID: directoryID(-1)}, {PreviousChannelServerID: directoryID(250)}} {
		if p.validateDirectory() == nil {
			t.Fatal("invalid directory accepted")
		}
	}
}

func TestDirectoryPublicationPreservesLegacyPayloadAndStagedMigration(t *testing.T) {
	h, _ := fixture(t)
	// New fields must stay omitted for old profiles: adding the schema must not
	// silently alter a signed payload at its already published sequence number.
	before, err := json.Marshal(h.state.Published)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(before), "ChannelServerId") {
		t.Fatal("legacy payload changed")
	}
	p := h.profile
	p.ChannelServerID, p.PreviousChannelServerID = 1, directoryID(0)
	encoded, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(h.cfg.ProfileFile, encoded, 0600); err != nil {
		t.Fatal(err)
	}
	h, err = New(h.cfg)
	if err != nil {
		t.Fatal(err)
	}
	stillPublished, _ := json.Marshal(h.state.Published)
	if string(before) != string(stillPublished) {
		t.Fatal("config edit silently republished metadata")
	}
	if h.profile.ChannelServerID != 1 || h.profile.PreviousChannelServerID == nil || *h.profile.PreviousChannelServerID != 0 {
		t.Fatal("configured migration lost")
	}
	// Deployment must deliberately publish the profile with a new sequence.
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
	var env Envelope
	if err = json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(env.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var b Bootstrap
	if err = json.Unmarshal(raw, &b); err != nil {
		t.Fatal(err)
	}
	if b.Sequence != 2 || b.Profile.ChannelServerID != 1 || b.Profile.PreviousChannelServerID == nil || *b.Profile.PreviousChannelServerID != 0 {
		t.Fatal("signed migration lost after restart")
	}
}

func TestDirectoryInvalidPersistedMetadataRejectedBeforeSave(t *testing.T) {
	h, _ := fixture(t)
	h.state.Published.Profile.PreviousChannelServerID = directoryID(0)
	if err := h.save(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.cfg.DataDir, "state.json")
	before, _ := os.ReadFile(path)
	if _, err := New(h.cfg); err == nil {
		t.Fatal("invalid persisted identity accepted")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("rejected state was modified")
	}
}
