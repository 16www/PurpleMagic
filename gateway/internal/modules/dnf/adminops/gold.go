package adminops

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

// The current game wallet notifications encode character gold as signed int32.
// Carry-gold purchase stages are a separate gameplay feature.
const MaxCharacterGold int64 = math.MaxInt32

var ErrGoldInput = errors.New("invalid character gold operation")
var ErrGoldBalance = errors.New("invalid stored character gold balance")

type GoldCommand struct {
	AccountID   string `json:"accountId"`
	CharacterID string `json:"characterId"`
	Action      string `json:"action"`
	Amount      string `json:"amount"`
	Revision    string `json:"revision"`
	Reason      string `json:"reason"`
	Actor       string `json:"actor"`
	PlanToken   string `json:"planToken,omitempty"`
	Key         string `json:"-"`
}
type GoldReceipt struct {
	RequestID   string    `json:"requestId,omitempty"`
	Kind        string    `json:"kind"`
	AccountID   string    `json:"accountId"`
	CharacterID string    `json:"characterId"`
	Action      string    `json:"action"`
	Amount      string    `json:"amount"`
	Delta       string    `json:"delta"`
	Before      string    `json:"before"`
	After       string    `json:"after"`
	Revision    string    `json:"revision"`
	Reason      string    `json:"reason"`
	Actor       string    `json:"actor"`
	CreatedAt   time.Time `json:"createdAt"`
}
type goldStored struct {
	Receipt     GoldReceipt `json:"receipt"`
	ContentHash string      `json:"contentHash"`
}

func CharacterGoldBalance(c repository.CharacterRecord) (int64, error) {
	b, present := c.Stats["gold"]
	if !present || b < 0 || b > MaxCharacterGold || c.Stats["gm_gold_sequence_v1"] < 0 {
		return 0, ErrGoldBalance
	}
	return b, nil
}
func CharacterGoldRevision(c repository.CharacterRecord) string {
	raw, _ := json.Marshal([]any{c.CharacterID, c.AccountID, c.Stats["gold"], c.Stats["gm_gold_sequence_v1"], c.Stats["delete_flag"], c.UpdatedAt.UTC()})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func GoldIdentity(c GoldCommand) string {
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func validGoldCommand(c GoldCommand, commit bool) error {
	id, err := strconv.ParseUint(c.CharacterID, 10, 16)
	amount, e := strconv.ParseInt(c.Amount, 10, 64)
	if err != nil || id == 0 || strconv.FormatUint(id, 10) != c.CharacterID || c.AccountID == "" || c.Actor == "" || e != nil || strconv.FormatInt(amount, 10) != c.Amount || amount < 0 || amount > MaxCharacterGold || len(c.Reason) > 500 || strings.TrimSpace(c.Reason) == "" || len(c.Revision) != 64 {
		return ErrGoldInput
	}
	if _, err = hex.DecodeString(c.Revision); err != nil {
		return ErrGoldInput
	}
	switch c.Action {
	case "add", "subtract":
		if amount == 0 {
			return ErrGoldInput
		}
	case "set":
	default:
		return ErrGoldInput
	}
	if commit && (len(c.Key) < 16 || len(c.Key) > 128) {
		return ErrGoldInput
	}
	return nil
}
func loadGoldReceipt(ctx context.Context, receipts repository.ManagementReceiptStore, c GoldCommand) (GoldReceipt, bool, error) {
	var old goldStored
	raw, found, err := receipts.Load(ctx, c.Key)
	if err != nil || !found {
		return old.Receipt, found, err
	}
	if err = json.Unmarshal(raw, &old); err != nil {
		return old.Receipt, true, err
	}
	if old.ContentHash != GoldIdentity(c) {
		return GoldReceipt{}, true, ErrIdempotency
	}
	return old.Receipt, true, nil
}

// Recovery precedes runtime checks and preview expiry; the owner and complete
// original command still have to match the committed receipt.
func LookupGoldOperation(ctx context.Context, u repository.CharacterGoldUnitOfWork, c GoldCommand) (GoldReceipt, bool, error) {
	var result GoldReceipt
	var found bool
	if err := validGoldCommand(c, true); err != nil {
		return result, false, err
	}
	if u == nil {
		return result, false, ErrRuntimeUnavailable
	}
	err := u.WithinCharacterGold(ctx, c.AccountID, c.CharacterID, func(g repository.CharacterGoldRepository, r repository.ManagementReceiptStore) error {
		ch, exists, e := g.LoadCharacterGold(ctx, c.CharacterID)
		if e != nil {
			return e
		}
		if !exists || ch.CharacterID != c.CharacterID || ch.AccountID != c.AccountID || ch.Stats["delete_flag"] != 0 {
			return ErrAccount
		}
		result, found, e = loadGoldReceipt(ctx, r, c)
		return e
	})
	return result, found, err
}

func RunGoldOperation(ctx context.Context, u repository.CharacterGoldUnitOfWork, c GoldCommand, commit bool, offline QuestOfflineCheck) (GoldReceipt, error) {
	var result GoldReceipt
	if e := validGoldCommand(c, commit); e != nil {
		return result, e
	}
	if u == nil {
		return result, ErrRuntimeUnavailable
	}
	err := u.WithinCharacterGold(ctx, c.AccountID, c.CharacterID, func(g repository.CharacterGoldRepository, r repository.ManagementReceiptStore) error {
		ch, found, e := g.LoadCharacterGold(ctx, c.CharacterID)
		if e != nil {
			return e
		}
		if !found || ch.CharacterID != c.CharacterID || ch.AccountID != c.AccountID || ch.Stats["delete_flag"] != 0 {
			return ErrAccount
		}
		if commit {
			old, exists, e := loadGoldReceipt(ctx, r, c)
			if e != nil {
				return e
			}
			if exists {
				result = old
				return nil
			}
		}
		before, e := CharacterGoldBalance(ch)
		if e != nil {
			return e
		}
		if CharacterGoldRevision(ch) != c.Revision {
			return ErrConflict
		}
		if ch.Stats["gm_gold_sequence_v1"] == math.MaxInt64 {
			return ErrGoldBalance
		}
		amount, _ := strconv.ParseInt(c.Amount, 10, 64)
		after := amount
		if c.Action == "add" {
			after = before + amount
		} else if c.Action == "subtract" {
			after = before - amount
		}
		if after < 0 || after > MaxCharacterGold {
			return ErrGoldInput
		}
		result = GoldReceipt{Kind: "character-gold-operation", AccountID: c.AccountID, CharacterID: c.CharacterID, Action: c.Action, Amount: c.Amount, Delta: strconv.FormatInt(after-before, 10), Before: strconv.FormatInt(before, 10), After: strconv.FormatInt(after, 10), Revision: c.Revision, Reason: c.Reason, Actor: c.Actor, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
		if !commit {
			return nil
		}
		if offline == nil {
			return ErrRuntimeUnavailable
		}
		if e = offline(ctx, c.CharacterID); e != nil {
			return e
		}
		ch = repository.CloneCharacter(ch)
		ch.Stats["gold"] = after
		ch.Stats["gm_gold_sequence_v1"]++
		if result.CreatedAt.After(ch.UpdatedAt) {
			ch.UpdatedAt = result.CreatedAt
		} else {
			ch.UpdatedAt = ch.UpdatedAt.Add(time.Microsecond)
		}
		if e = g.SaveCharacterGold(ctx, ch); e != nil {
			return e
		}
		if e = offline(ctx, c.CharacterID); e != nil {
			return e
		}
		persisted, found, e := g.LoadCharacterGold(ctx, c.CharacterID)
		if e != nil {
			return e
		}
		if !found || persisted.AccountID != c.AccountID || persisted.Stats["gold"] != after || persisted.Stats["gm_gold_sequence_v1"] != ch.Stats["gm_gold_sequence_v1"] {
			return ErrConflict
		}
		result.Revision = CharacterGoldRevision(persisted)
		result.RequestID = c.Key
		raw, e := json.Marshal(goldStored{Receipt: result, ContentHash: GoldIdentity(c)})
		if e != nil {
			return e
		}
		return r.Append(ctx, c.Key, raw)
	})
	return result, err
}

func DecodeGoldReceipt(raw []byte) (GoldReceipt, error) {
	var old goldStored
	err := json.Unmarshal(raw, &old)
	if err == nil && old.Receipt.Kind != "character-gold-operation" {
		err = errors.New("invalid gold receipt kind")
	}
	return old.Receipt, err
}
