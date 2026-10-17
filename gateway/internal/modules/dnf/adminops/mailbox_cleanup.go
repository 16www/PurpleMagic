package adminops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

var ErrCleanupMissing = errors.New("mailbox cleanup missing")
var cleanupKey = regexp.MustCompile(`^[A-Za-z0-9_-]{16,96}$`)

type MailboxCleanupCommand struct {
	AccountID        string                   `json:"accountId"`
	CharacterID      string                   `json:"characterId"`
	Filter           repository.MailboxFilter `json:"filter"`
	DiscardUnclaimed bool                     `json:"discardUnclaimed"`
	Reason           string                   `json:"reason"`
	Actor            string                   `json:"actor"`
	Key              string                   `json:"-"`
}
type MailboxCleanup struct {
	ID               string                   `json:"id"`
	AccountID        string                   `json:"accountId"`
	CharacterID      string                   `json:"characterId"`
	Filter           repository.MailboxFilter `json:"filter"`
	DiscardUnclaimed bool                     `json:"discardUnclaimed"`
	Reason           string                   `json:"reason"`
	Actor            string                   `json:"actor"`
	Status           string                   `json:"status"`
	Revision         uint64                   `json:"revision"`
	Total            int64                    `json:"total"`
	Processed        int64                    `json:"processed"`
	Unclaimed        int64                    `json:"unclaimed"`
	Gold             string                   `json:"gold"`
	Attachments      int64                    `json:"attachments"`
	CreatedAt        time.Time                `json:"createdAt"`
	UpdatedAt        time.Time                `json:"updatedAt"`
}
type cleanupStored struct {
	Job        MailboxCleanup `json:"job"`
	Identity   string         `json:"identity"`
	After      string         `json:"after"`
	SnapshotAt time.Time      `json:"snapshotAt"`
	ExpectedAt time.Time      `json:"expectedAt"`
}

func cleanupIdentity(v MailboxCleanupCommand) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func CleanupPlanIdentity(v MailboxCleanup) string {
	raw, _ := json.Marshal(v)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func DecodeMailboxCleanup(raw []byte) (MailboxCleanup, error) {
	var v cleanupStored
	e := json.Unmarshal(raw, &v)
	if e == nil && (!cleanupKey.MatchString(v.Job.ID) || v.Job.Revision == 0) {
		e = ErrMailboxInput
	}
	return v.Job, e
}
func loadCleanup(ctx context.Context, s repository.MailboxCleanupStore, account, character, id string) (cleanupStored, error) {
	var v cleanupStored
	raw, found, e := s.LoadCleanup(ctx, id)
	if e != nil {
		return v, e
	}
	if !found {
		return v, ErrCleanupMissing
	}
	if e = json.Unmarshal(raw, &v); e != nil {
		return v, e
	}
	if v.Job.ID != id || v.Job.AccountID != account || v.Job.CharacterID != character {
		return v, ErrAccount
	}
	return v, nil
}
func saveCleanup(ctx context.Context, s repository.MailboxCleanupStore, v *cleanupStored) error {
	v.Job.Revision++
	v.Job.UpdatedAt = time.Now().UTC().Truncate(time.Microsecond)
	raw, e := json.Marshal(v)
	if e != nil {
		return e
	}
	return s.SaveCleanup(ctx, v.Job.ID, raw)
}

func CreateMailboxCleanup(ctx context.Context, u repository.MailboxManagementUnitOfWork, c MailboxCleanupCommand) (MailboxCleanup, error) {
	var out MailboxCleanup
	var e error
	c.Filter, e = c.Filter.Normalize()
	if e != nil {
		return out, ErrMailboxInput
	}
	if !cleanupKey.MatchString(c.Key) || c.Actor == "" || strings.TrimSpace(c.Reason) == "" || len(c.Reason) > 500 || c.AccountID == "" {
		return out, ErrMailboxInput
	}
	n, e := strconv.ParseUint(c.CharacterID, 10, 16)
	if e != nil || n == 0 || strconv.FormatUint(n, 10) != c.CharacterID {
		return out, ErrMailboxInput
	}
	if u == nil {
		return out, ErrRuntimeUnavailable
	}
	e = u.WithinMailboxManagement(ctx, c.AccountID, c.CharacterID, func(repo repository.MailboxManagementRepository, _ repository.ManagementReceiptStore) error {
		s, ok := repo.(repository.MailboxCleanupStore)
		if !ok {
			return ErrRuntimeUnavailable
		}
		v, e := loadCleanup(ctx, s, c.AccountID, c.CharacterID, c.Key)
		if e == nil {
			if v.Identity != cleanupIdentity(c) {
				return ErrIdempotency
			}
			out = v.Job
			return nil
		}
		if !errors.Is(e, ErrCleanupMissing) {
			return e
		}
		v = cleanupStored{Job: MailboxCleanup{ID: c.Key, AccountID: c.AccountID, CharacterID: c.CharacterID, Filter: c.Filter, DiscardUnclaimed: c.DiscardUnclaimed, Reason: c.Reason, Actor: c.Actor, Status: "building", Gold: "0", CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}, Identity: cleanupIdentity(c), SnapshotAt: s.MailboxVersion(), ExpectedAt: s.MailboxVersion()}
		if e = prepareCleanup(ctx, repo, s, &v); e != nil {
			return e
		}
		if e = saveCleanup(ctx, s, &v); e != nil {
			return e
		}
		out = v.Job
		return nil
	})
	return out, e
}

func prepareCleanup(ctx context.Context, repo repository.MailboxManagementRepository, s repository.MailboxCleanupStore, v *cleanupStored) error {
	rows, e := s.CleanupCandidates(ctx, v.Job.Filter, v.After, 101)
	if e != nil {
		return e
	}
	more := len(rows) > 100
	if more {
		rows = rows[:100]
	}
	ids := []string{}
	for _, m := range rows {
		ids = append(ids, m.MailID)
	}
	if len(ids) > 0 {
		state, e := repo.LoadManagedMails(ctx, ids)
		if e != nil {
			return e
		}
		items := []repository.MailboxCleanupItem{}
		gold, ok := new(big.Int).SetString(v.Job.Gold, 10)
		if !ok {
			return ErrMailboxInput
		}
		for _, id := range ids {
			m := state.Mails[id]
			unclaimed := !m.Claimed && (m.Gold > 0 || len(m.Attachments) > 0)
			change := MailboxChange{MailID: id, Revision: ManagedMailRevision(v.Job.CharacterID, v.SnapshotAt, m), Title: m.Title, Claimed: m.Claimed, Gold: strconv.FormatInt(m.Gold, 10), Attachments: m.Attachments, DiscardedAssets: unclaimed}
			raw, e := json.Marshal(change)
			if e != nil {
				return e
			}
			v.Job.Total++
			items = append(items, repository.MailboxCleanupItem{Ordinal: v.Job.Total, Payload: raw})
			if unclaimed {
				v.Job.Unclaimed++
				if m.Gold > 0 {
					gold.Add(gold, big.NewInt(m.Gold))
				}
				v.Job.Attachments += int64(len(m.Attachments))
			}
		}
		v.Job.Gold = gold.String()
		v.After = ids[len(ids)-1]
		if e = s.AppendCleanupItems(ctx, v.Job.ID, items); e != nil {
			return e
		}
	}
	if !more {
		v.Job.Status = "ready"
	}
	return nil
}

// Each advance performs at most one page and atomically checkpoints progress.
// An old revision is a replay/readback, including after a lost response.
func AdvanceMailboxCleanup(ctx context.Context, u repository.MailboxManagementUnitOfWork, account, character, id, mode, actor string, expected uint64, confirm func(MailboxCleanup) error, offline QuestOfflineCheck) (MailboxCleanup, error) {
	var out MailboxCleanup
	if u == nil {
		return out, ErrRuntimeUnavailable
	}
	if !cleanupKey.MatchString(id) || expected == 0 || actor == "" || (mode != "prepare" && mode != "run" && mode != "cancel") {
		return out, ErrMailboxInput
	}
	e := u.WithinMailboxManagement(ctx, account, character, func(repo repository.MailboxManagementRepository, r repository.ManagementReceiptStore) error {
		s, ok := repo.(repository.MailboxCleanupStore)
		if !ok {
			return ErrRuntimeUnavailable
		}
		v, e := loadCleanup(ctx, s, account, character, id)
		if e != nil {
			return e
		}
		out = v.Job
		if expected > v.Job.Revision {
			return ErrConflict
		}
		if expected < v.Job.Revision || v.Job.Status == "completed" || v.Job.Status == "cancelled" || v.Job.Status == "conflict" {
			return nil
		}
		if mode == "cancel" {
			v.Job.Status = "cancelled"
		} else {
			if !v.ExpectedAt.Equal(s.MailboxVersion()) {
				v.Job.Status = "conflict"
			} else if mode == "prepare" {
				if v.Job.Status != "building" {
					return ErrConflict
				}
				if e = prepareCleanup(ctx, repo, s, &v); e != nil {
					return e
				}
			} else {
				if v.Job.Status != "ready" && v.Job.Status != "running" {
					return ErrConflict
				}
				if v.Job.Status == "ready" {
					if confirm == nil {
						return ErrConflict
					}
					if e = confirm(v.Job); e != nil {
						return e
					}
				}
				if v.Job.Unclaimed > 0 && !v.Job.DiscardUnclaimed {
					return ErrUnclaimedMail
				}
				if e = runCleanupBatch(ctx, repo, s, r, &v, actor, offline); e != nil {
					return e
				}
			}
		}
		if e = saveCleanup(ctx, s, &v); e != nil {
			return e
		}
		out = v.Job
		return nil
	})
	return out, e
}

func runCleanupBatch(ctx context.Context, repo repository.MailboxManagementRepository, s repository.MailboxCleanupStore, r repository.ManagementReceiptStore, v *cleanupStored, actor string, offline QuestOfflineCheck) error {
	if v.Job.Processed == v.Job.Total {
		v.Job.Status = "completed"
		return nil
	}
	items, e := s.CleanupItems(ctx, v.Job.ID, v.Job.Processed, 100)
	if e != nil {
		return e
	}
	if len(items) == 0 {
		return ErrMailboxInput
	}
	changes := []MailboxChange{}
	ids := []string{}
	for i, item := range items {
		if item.Ordinal != v.Job.Processed+int64(i)+1 {
			return ErrMailboxInput
		}
		var c MailboxChange
		if e = json.Unmarshal(item.Payload, &c); e != nil {
			return e
		}
		changes = append(changes, c)
		ids = append(ids, c.MailID)
	}
	state, e := repo.LoadManagedMails(ctx, ids)
	if errors.Is(e, repository.ErrManagedMailMissing) {
		v.Job.Status = "conflict"
		return nil
	}
	if e != nil {
		return e
	}
	for _, c := range changes {
		m := state.Mails[c.MailID]
		if ManagedMailRevision(v.Job.CharacterID, v.SnapshotAt, m) != c.Revision {
			v.Job.Status = "conflict"
			return nil
		}
	}
	if offline == nil {
		return ErrRuntimeUnavailable
	}
	if e = offline(ctx, v.Job.CharacterID); e != nil {
		return e
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	if !at.After(state.UpdatedAt) {
		at = state.UpdatedAt.Add(time.Microsecond)
	}
	if e = repo.DeleteManagedMails(ctx, ids, at); e != nil {
		return e
	}
	if e = offline(ctx, v.Job.CharacterID); e != nil {
		return e
	}
	digest := sha256.Sum256([]byte(v.Job.ID))
	key := fmt.Sprintf("cleanup-%x-%d", digest, v.Job.Processed)
	receipt := MailboxReceipt{RequestID: key, Kind: "mailbox-delete-operation", AccountID: v.Job.AccountID, CharacterID: v.Job.CharacterID, Changes: changes, DiscardUnclaimed: v.Job.DiscardUnclaimed, Reason: v.Job.Reason, Actor: actor, CreatedAt: at, CleanupID: v.Job.ID}
	raw, e := json.Marshal(mailboxStored{Receipt: receipt, ContentHash: CleanupPlanIdentity(v.Job)})
	if e != nil {
		return e
	}
	if e = r.Append(ctx, key, raw); e != nil {
		return e
	}
	v.Job.Processed += int64(len(items))
	v.ExpectedAt = at
	v.Job.Status = "running"
	if v.Job.Processed == v.Job.Total {
		v.Job.Status = "completed"
	}
	return nil
}

func ReadMailboxCleanup(ctx context.Context, u repository.MailboxManagementUnitOfWork, account, character, id string, after int64, limit int) (MailboxCleanup, []repository.MailboxCleanupItem, error) {
	var out MailboxCleanup
	var items []repository.MailboxCleanupItem
	if u == nil {
		return out, nil, ErrRuntimeUnavailable
	}
	if !cleanupKey.MatchString(id) || after < 0 || limit < 0 || limit > 101 {
		return out, nil, ErrMailboxInput
	}
	e := u.WithinMailboxManagement(ctx, account, character, func(repo repository.MailboxManagementRepository, _ repository.ManagementReceiptStore) error {
		s, ok := repo.(repository.MailboxCleanupStore)
		if !ok {
			return ErrRuntimeUnavailable
		}
		v, e := loadCleanup(ctx, s, account, character, id)
		if e != nil {
			return e
		}
		out = v.Job
		if limit > 0 {
			items, e = s.CleanupItems(ctx, id, after, limit)
		}
		return e
	})
	return out, items, e
}
