package testbenefits

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"dnf90.local/gateway/internal/modules/dnf/repository"
)

// Reward content is immutable. Repeated previews and automatic deliveries share
// its ID; enabling a rule never resets receipts or creates a replacement rule.
type Campaign struct {
	RequestFingerprint string    `json:"requestFingerprint,omitempty"`
	ID                 string    `json:"id"`
	Revision           uint64    `json:"revision"`
	CreatedAt          time.Time `json:"createdAt"`
	FirstEnabledAt     time.Time `json:"firstEnabledAt"`
	Enabled            bool      `json:"enabled"`
	AutoNewCharacters  bool      `json:"autoNewCharacters"`
	ItemName           string    `json:"itemName"`
	Reward             Job       `json:"reward"`
}

func (s *Store) Campaign(ctx context.Context, id string) (Campaign, error) {
	if !validCampaignID(id) {
		return Campaign{}, errors.New("无效的福利规则编号")
	}
	raw, _, err := s.readPayload(ctx, s.db, "campaigns", id, "")
	var c Campaign
	if err == nil {
		err = json.Unmarshal(raw, &c)
	}
	return c, err
}

func (s *Store) Campaigns(ctx context.Context) ([]Campaign, error) {
	rows, err := s.db.QueryContext(ctx, s.payloadListQuery("campaigns"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Campaign{}
	for rows.Next() {
		var raw []byte
		var c Campaign
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &c); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

func (s *Store) CreateCampaign(ctx context.Context, reward Job, name string, automatic bool) (Campaign, error) {
	if reward.RewardPlan != nil {
		return Campaign{}, ErrRewardProjection
	}
	if err := reward.Validate(); err != nil {
		return Campaign{}, err
	}
	if len(reward.Attachments) != 1 {
		return Campaign{}, errors.New("物品福利需要一种已核对的物品")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return Campaign{}, err
	}
	now := time.Now().UTC()
	key := hex.EncodeToString(id[:])
	// Only approved content survives; never accept IDs, recipients or prior run
	// status supplied by a caller as part of a new rule.
	reward = Job{ID: key, CampaignID: key, Scope: reward.Scope.Effective(), Gold: reward.Gold, Title: reward.Title, Body: reward.Body, Attachments: reward.Attachments, CreatedAt: now}
	c := Campaign{ID: key, CreatedAt: now, AutoNewCharacters: automatic, ItemName: name, Reward: reward}
	raw, err := json.Marshal(c)
	if err != nil {
		return Campaign{}, err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO "+s.scopedTable("campaigns", reward.Scope)+" (id,created_at,payload) VALUES (?,?,?)", key, now, raw)
	return c, err
}

func (s *Store) EnableCampaign(ctx context.Context, id string, revision uint64, enabled bool) (Campaign, error) {
	if !validCampaignID(id) {
		return Campaign{}, errors.New("无效的福利规则编号")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Campaign{}, err
	}
	defer tx.Rollback()
	raw, table, err := s.readPayload(ctx, tx, "campaigns", id, " FOR UPDATE")
	if err != nil {
		return Campaign{}, err
	}
	var c Campaign
	if err = json.Unmarshal(raw, &c); err != nil {
		return Campaign{}, err
	}
	if c.Revision != revision {
		return Campaign{}, ErrRevision
	}
	c.Enabled = enabled
	c.Revision++
	if enabled && c.FirstEnabledAt.IsZero() {
		c.FirstEnabledAt = time.Now().UTC()
	}
	raw, err = json.Marshal(c)
	if err != nil {
		return Campaign{}, err
	}
	if _, err = tx.ExecContext(ctx, "UPDATE "+table+" SET payload=? WHERE id=?", raw, id); err != nil {
		return Campaign{}, err
	}
	return c, tx.Commit()
}

func (s *Store) PreviewCampaign(ctx context.Context, group repository.Group, id string) (Job, error) {
	c, err := s.Campaign(ctx, id)
	if err != nil {
		return Job{}, err
	}
	return s.Preview(ctx, group, c.Reward)
}

// Hold a shared rule lock until delivery finishes. Disable waits for in-flight
// batches, then prevents new manual and automatic deliveries.
func (s *Store) lockCampaign(ctx context.Context, tx *sql.Tx, id string) (Campaign, error) {
	raw, _, err := s.readPayload(ctx, tx, "campaigns", id, " LOCK IN SHARE MODE")
	var c Campaign
	if err == nil {
		err = json.Unmarshal(raw, &c)
	}
	return c, err
}

func EligibleNewCharacter(c Campaign, character repository.CharacterRecord) bool {
	return c.Enabled && c.AutoNewCharacters && !c.FirstEnabledAt.IsZero() &&
		!character.CreatedAt.IsZero() && !character.CreatedAt.Before(c.FirstEnabledAt) && character.Stats["delete_flag"] == 0
}

// Called at character login before mailbox projection. If a process dies or a
// transaction fails, the next login retries; committed receipts survive claims.
func (s *Store) DeliverNewCharacter(ctx context.Context, group repository.Group, character repository.CharacterRecord) error {
	return s.DeliverNewCharacterProjected(ctx, group, character, nil)
}

func (s *Store) DeliverNewCharacterProjected(ctx context.Context, group repository.Group, character repository.CharacterRecord, project RewardProjector) error {
	list, err := s.Campaigns(ctx)
	if err != nil {
		return err
	}
	for _, c := range list {
		if !EligibleNewCharacter(c, character) {
			continue
		}
		err = s.DeliverCampaignToNewCharacter(ctx, group, c.ID, character, project)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) DeliverCampaignToNewCharacter(ctx context.Context, group repository.Group, id string, character repository.CharacterRecord, project RewardProjector) error {
	if !validCampaignID(id) {
		return errors.New("无效的福利规则编号")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	c, err := s.lockCampaign(ctx, tx, id)
	if err != nil {
		return err
	}
	if EligibleNewCharacter(c, character) {
		_, err = DeliverProjectedWithReceipt(ctx, group, c.Reward, Recipient{AccountID: character.AccountID, CharacterID: character.CharacterID, Name: character.Name, CreatedAt: character.CreatedAt}, project)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
