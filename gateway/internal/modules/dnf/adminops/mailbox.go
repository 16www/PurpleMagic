package adminops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

var ErrMailboxInput = errors.New("invalid mailbox operation")
var ErrUnclaimedMail = errors.New("selected mail contains unclaimed assets")

type MailSelection struct {
	MailID   string `json:"mailId"`
	Revision string `json:"revision,omitempty"`
}
type MailboxCommand struct {
	AccountID        string          `json:"accountId"`
	CharacterID      string          `json:"characterId"`
	Items            []MailSelection `json:"items"`
	DiscardUnclaimed bool            `json:"discardUnclaimed"`
	Reason           string          `json:"reason"`
	Actor            string          `json:"actor"`
	PlanToken        string          `json:"planToken,omitempty"`
	Key              string          `json:"-"`
}
type MailboxChange struct {
	MailID          string                      `json:"mailId"`
	Revision        string                      `json:"revision"`
	Title           string                      `json:"title"`
	Claimed         bool                        `json:"claimed"`
	Gold            string                      `json:"gold"`
	Attachments     []repository.MailAttachment `json:"attachments"`
	DiscardedAssets bool                        `json:"discardedAssets"`
}
type MailboxReceipt struct {
	CleanupID        string          `json:"cleanupId,omitempty"`
	RequestID        string          `json:"requestId,omitempty"`
	Kind             string          `json:"kind"`
	AccountID        string          `json:"accountId"`
	CharacterID      string          `json:"characterId"`
	Changes          []MailboxChange `json:"changes"`
	DiscardUnclaimed bool            `json:"discardUnclaimed"`
	Reason           string          `json:"reason"`
	Actor            string          `json:"actor"`
	CreatedAt        time.Time       `json:"createdAt"`
}
type mailboxStored struct {
	Receipt     MailboxReceipt `json:"receipt"`
	ContentHash string         `json:"contentHash"`
}

func MailboxIdentity(c MailboxCommand) string {
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func ManagedMailRevision(id string, at time.Time, m repository.MailRecord) string {
	raw, _ := json.Marshal([]any{id, at.UTC(), m})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func validMailboxCommand(c MailboxCommand, commit bool) error {
	n, e := strconv.ParseUint(c.CharacterID, 10, 16)
	if e != nil || n == 0 || strconv.FormatUint(n, 10) != c.CharacterID || c.AccountID == "" || c.Actor == "" || strings.TrimSpace(c.Reason) == "" || len(c.Reason) > 500 || len(c.Items) == 0 || len(c.Items) > 100 {
		return ErrMailboxInput
	}
	seen := map[string]bool{}
	for _, item := range c.Items {
		if item.MailID == "" || len(item.MailID) > 128 || strings.TrimSpace(item.MailID) != item.MailID || seen[item.MailID] {
			return ErrMailboxInput
		}
		seen[item.MailID] = true
		if commit || item.Revision != "" {
			b, e := hex.DecodeString(item.Revision)
			if e != nil || len(b) != 32 {
				return ErrMailboxInput
			}
		}
	}
	if commit && (len(c.Key) < 16 || len(c.Key) > 128) {
		return ErrMailboxInput
	}
	return nil
}
func loadMailboxReceipt(ctx context.Context, r repository.ManagementReceiptStore, c MailboxCommand) (MailboxReceipt, bool, error) {
	var old mailboxStored
	raw, found, e := r.Load(ctx, c.Key)
	if e != nil || !found {
		return old.Receipt, found, e
	}
	if e = json.Unmarshal(raw, &old); e != nil {
		return old.Receipt, true, e
	}
	if old.ContentHash != MailboxIdentity(c) {
		return MailboxReceipt{}, true, ErrIdempotency
	}
	return old.Receipt, true, nil
}
func LookupMailboxOperation(ctx context.Context, u repository.MailboxManagementUnitOfWork, c MailboxCommand) (MailboxReceipt, bool, error) {
	var result MailboxReceipt
	var found bool
	if e := validMailboxCommand(c, true); e != nil {
		return result, false, e
	}
	if u == nil {
		return result, false, ErrRuntimeUnavailable
	}
	e := u.WithinMailboxManagement(ctx, c.AccountID, c.CharacterID, func(_ repository.MailboxManagementRepository, r repository.ManagementReceiptStore) error {
		var e error
		result, found, e = loadMailboxReceipt(ctx, r, c)
		return e
	})
	return result, found, e
}
func RunMailboxOperation(ctx context.Context, u repository.MailboxManagementUnitOfWork, c MailboxCommand, commit bool, offline QuestOfflineCheck) (MailboxReceipt, error) {
	var result MailboxReceipt
	if e := validMailboxCommand(c, commit); e != nil {
		return result, e
	}
	if u == nil {
		return result, ErrRuntimeUnavailable
	}
	e := u.WithinMailboxManagement(ctx, c.AccountID, c.CharacterID, func(repo repository.MailboxManagementRepository, r repository.ManagementReceiptStore) error {
		if commit {
			old, found, e := loadMailboxReceipt(ctx, r, c)
			if e != nil {
				return e
			}
			if found {
				result = old
				return nil
			}
		}
		ids := make([]string, len(c.Items))
		for i, item := range c.Items {
			ids[i] = item.MailID
		}
		state, e := repo.LoadManagedMails(ctx, ids)
		if e != nil {
			return e
		}
		result = MailboxReceipt{Kind: "mailbox-delete-operation", AccountID: c.AccountID, CharacterID: c.CharacterID, DiscardUnclaimed: c.DiscardUnclaimed, Reason: c.Reason, Actor: c.Actor, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), Changes: []MailboxChange{}}
		for _, item := range c.Items {
			m, found := state.Mails[item.MailID]
			if !found || m.Deleted {
				return repository.ErrManagedMailMissing
			}
			version := ManagedMailRevision(c.CharacterID, state.UpdatedAt, m)
			if item.Revision != "" && version != item.Revision {
				return ErrConflict
			}
			unclaimed := !m.Claimed && (m.Gold > 0 || len(m.Attachments) > 0)
			if unclaimed && !c.DiscardUnclaimed {
				return ErrUnclaimedMail
			}
			result.Changes = append(result.Changes, MailboxChange{MailID: m.MailID, Revision: version, Title: m.Title, Claimed: m.Claimed, Gold: strconv.FormatInt(m.Gold, 10), Attachments: m.Attachments, DiscardedAssets: unclaimed})
		}
		if !commit {
			return nil
		}
		if offline == nil {
			return ErrRuntimeUnavailable
		}
		if e = offline(ctx, c.CharacterID); e != nil {
			return e
		}
		at := result.CreatedAt
		if !at.After(state.UpdatedAt) {
			at = state.UpdatedAt.Add(time.Microsecond)
		}
		if e = repo.DeleteManagedMails(ctx, ids, at); e != nil {
			return e
		}
		if e = offline(ctx, c.CharacterID); e != nil {
			return e
		}
		result.RequestID = c.Key
		raw, e := json.Marshal(mailboxStored{Receipt: result, ContentHash: MailboxIdentity(c)})
		if e != nil {
			return e
		}
		return r.Append(ctx, c.Key, raw)
	})
	return result, e
}
func DecodeMailboxReceipt(raw []byte) (MailboxReceipt, error) {
	var v mailboxStored
	e := json.Unmarshal(raw, &v)
	if e == nil && v.Receipt.Kind != "mailbox-delete-operation" {
		e = errors.New("invalid mailbox receipt")
	}
	return v.Receipt, e
}
