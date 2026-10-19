package testbenefits

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

var ErrMailSelection = errors.New("请选择账号或角色；各最多100个，展开后最多5000个角色，并核对物品和发放参数")

type MailTarget struct {
	AccountID   string `json:"accountId"`
	CharacterID string `json:"characterId"`
}

type MailOptions struct {
	ExpireDays       int    `json:"expireDays"`
	Reinforce        int    `json:"reinforce"`
	AmplifyType      int    `json:"amplifyType"`
	EquipmentQuality string `json:"equipmentQuality"`
	ForgingLevel     int    `json:"forgingLevel"`
}

// MailDraft contains intent only. Resolved instance fields are created once
// inside createOnce, after checking for an existing content-bound request.
type MailDraft struct {
	AccountIDs  []string     `json:"accountIds,omitempty"`
	Recipients  []MailTarget `json:"recipients"`
	Title       string       `json:"title"`
	Body        string       `json:"body"`
	Gold        int64        `json:"gold"`
	ItemID      string       `json:"itemId"`
	Count       int64        `json:"count"`
	Options     MailOptions  `json:"options"`
	PVFChecksum string       `json:"pvfChecksum"`
}

func (s *Store) PreviewSelectedMailOnce(ctx context.Context, group repository.Group, key string, draft MailDraft, project func(MailDraft, time.Time) (Job, error)) (Job, error) {
	if len(draft.Recipients)+len(draft.AccountIDs) == 0 || len(draft.Recipients) > 100 || len(draft.AccountIDs) > 100 || project == nil {
		return Job{}, ErrMailSelection
	}
	draft.AccountIDs = append([]string(nil), draft.AccountIDs...)
	sort.Strings(draft.AccountIDs)
	for i, account := range draft.AccountIDs {
		if strings.TrimSpace(account) != account || account == "" || len(account) > 128 || (i > 0 && draft.AccountIDs[i-1] == account) {
			return Job{}, ErrMailSelection
		}
	}
	draft.Recipients = append([]MailTarget(nil), draft.Recipients...)
	seen := map[string]bool{}
	for _, r := range draft.Recipients {
		n, err := strconv.ParseUint(r.CharacterID, 10, 16)
		if err != nil || n == 0 || strconv.FormatUint(n, 10) != r.CharacterID || strings.TrimSpace(r.AccountID) == "" || len(r.AccountID) > 128 || seen[r.CharacterID] {
			return Job{}, ErrMailSelection
		}
		seen[r.CharacterID] = true
	}
	sort.Slice(draft.Recipients, func(i, j int) bool { return draft.Recipients[i].CharacterID < draft.Recipients[j].CharacterID })
	raw, err := s.createOnce(ctx, "jobs", key, draft, func(id, fp string) (GrantScope, any, error) {
		reader, ok := group.Character.(repository.CharacterSummaryReader)
		if !ok {
			return "", nil, errors.New("character summary reader unavailable")
		}
		// Expansion belongs inside createOnce: retries keep the original roster,
		// even if an account gains a character after confirmation.
		targets := append([]MailTarget(nil), draft.Recipients...)
		summaries := map[string]repository.CharacterSummary{}
		if len(draft.AccountIDs) > 0 {
			rows, err := reader.SummariesForAccounts(ctx, draft.AccountIDs)
			if err != nil {
				return "", nil, err
			}
			accounts := map[string]bool{}
			owners := map[string]string{}
			for _, account := range draft.AccountIDs {
				accounts[account] = true
			}
			for _, target := range targets {
				owners[target.CharacterID] = target.AccountID
			}
			for _, c := range rows {
				if !accounts[c.AccountID] || c.CharacterID == "" {
					return "", nil, ErrRecipient
				}
				summaries[c.CharacterID] = c
				if owner, exists := owners[c.CharacterID]; exists {
					if owner != c.AccountID {
						return "", nil, ErrRecipient
					}
					continue
				}
				owners[c.CharacterID] = c.AccountID
				targets = append(targets, MailTarget{AccountID: c.AccountID, CharacterID: c.CharacterID})
			}
		}
		if len(targets) == 0 {
			return "", nil, ErrPreviewEmpty
		}
		if len(targets) > MaxOnlineMailRecipients {
			return "", nil, ErrPreviewLimit
		}
		sort.Slice(targets, func(i, j int) bool { return targets[i].CharacterID < targets[j].CharacterID })
		now := time.Now().UTC().Truncate(time.Second)
		j, err := project(draft, now)
		if err != nil {
			return "", nil, err
		}
		if err = j.Validate(); err != nil {
			return "", nil, err
		}
		j.Scope, j.ID, j.CreatedAt, j.RequestFingerprint = ScopeCharacter, id, now, fp
		j.CampaignID, j.PVFChecksum = "", draft.PVFChecksum
		j.Recipients = make([]Recipient, 0, len(targets))
		for _, target := range targets {
			c, found := summaries[target.CharacterID]
			var err error
			if !found {
				c, found, err = reader.FindSummary(ctx, target.CharacterID)
			}
			if err != nil {
				return "", nil, err
			}
			if !found || c.AccountID != target.AccountID {
				return "", nil, ErrRecipient
			}
			j.Recipients = append(j.Recipients, Recipient{AccountID: c.AccountID, CharacterID: c.CharacterID, Name: c.Name, CreatedAt: c.CreatedAt})
		}
		return j.Scope, j, nil
	})
	var j Job
	if err == nil {
		err = json.Unmarshal(raw, &j)
	}
	return j, err
}
