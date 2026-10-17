package testbenefits

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

const MaxOnlineMailRecipients = 5000

var ErrOnlineSelection = errors.New("在线名单无效或不完整，请重新获取")

type RecipientSource struct {
	Kind       string    `json:"kind"`
	SnapshotID string    `json:"snapshotId"`
	ObservedAt time.Time `json:"observedAt"`
	Count      int       `json:"count"`
}
type OnlineMember struct {
	CharacterID string
	Generation  uint64
}
type OnlineSelection struct {
	Source  RecipientSource
	Members []OnlineMember
}

func (s *Store) PreviewOnlineMailOnce(ctx context.Context, g repository.Group, key string, draft MailDraft, load func(context.Context) (OnlineSelection, error), project func(MailDraft, time.Time) (Job, error)) (Job, error) {
	if len(draft.Recipients) != 0 || load == nil || project == nil {
		return Job{}, ErrOnlineSelection
	}
	raw, err := s.createOnce(ctx, "jobs", key, struct {
		Kind  string
		Draft MailDraft
	}{"online-mail-v1", draft}, func(id, fp string) (GrantScope, any, error) {
		now := time.Now().UTC().Truncate(time.Second)
		j, err := project(draft, now)
		if err != nil {
			return "", nil, err
		}
		if err = j.Validate(); err != nil {
			return "", nil, err
		}
		selection, err := load(ctx)
		if err != nil {
			return "", nil, err
		}
		source := selection.Source
		digest, e := hex.DecodeString(source.SnapshotID)
		if e != nil || len(digest) != 16 || source.Kind != "online" || source.ObservedAt.IsZero() || source.Count != len(selection.Members) || len(selection.Members) > MaxOnlineMailRecipients {
			return "", nil, ErrOnlineSelection
		}
		if len(selection.Members) == 0 {
			return "", nil, ErrPreviewEmpty
		}
		ids := make([]string, 0, len(selection.Members))
		generations := map[string]uint64{}
		for _, m := range selection.Members {
			n, e := strconv.ParseUint(m.CharacterID, 10, 16)
			if e != nil || n == 0 || strconv.FormatUint(n, 10) != m.CharacterID || m.Generation == 0 || generations[m.CharacterID] != 0 {
				return "", nil, ErrOnlineSelection
			}
			ids = append(ids, m.CharacterID)
			generations[m.CharacterID] = m.Generation
		}
		sort.Strings(ids)
		reader, ok := g.Character.(repository.CharacterBatchSummaryReader)
		if !ok {
			return "", nil, errors.New("batch character reader unavailable")
		}
		j.ID, j.Scope, j.CreatedAt, j.RequestFingerprint = id, ScopeCharacter, now, fp
		j.CampaignID, j.PVFChecksum = "", draft.PVFChecksum
		j.RecipientSource = &source
		j.Recipients = make([]Recipient, 0, len(ids))
		for start := 0; start < len(ids); start += 100 {
			end := min(start+100, len(ids))
			batch := ids[start:end]
			rows, err := reader.SummariesForCharacters(ctx, batch)
			if err != nil {
				return "", nil, err
			}
			if len(rows) != len(batch) {
				return "", nil, ErrRecipient
			}
			byID := map[string]repository.CharacterSummary{}
			for _, c := range rows {
				if _, dup := byID[c.CharacterID]; dup {
					return "", nil, ErrRecipient
				}
				byID[c.CharacterID] = c
			}
			for _, id := range batch {
				c, ok := byID[id]
				if !ok || c.AccountID == "" {
					return "", nil, ErrRecipient
				}
				j.Recipients = append(j.Recipients, Recipient{AccountID: c.AccountID, CharacterID: c.CharacterID, Name: c.Name, CreatedAt: c.CreatedAt, SessionGeneration: strconv.FormatUint(generations[id], 10)})
			}
		}
		return j.Scope, j, nil
	})
	var j Job
	if err == nil {
		err = json.Unmarshal(raw, &j)
	}
	return j, err
}
