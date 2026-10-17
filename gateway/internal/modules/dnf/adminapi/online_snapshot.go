package adminapi

import (
	"context"
	"encoding/hex"
	"errors"
	"time"
)

const MaxOnlineSnapshotMembers = 5000

type OnlineMember struct {
	CharacterID uint16 `json:"characterId"`
	Generation  uint64 `json:"generation,string"`
}

// A complete point-in-time view of the game process's canonical session index.
// No connection addresses, credentials, player assets or full session state.
type OnlineSnapshot struct {
	ID         string         `json:"id"`
	ObservedAt time.Time      `json:"observedAt"`
	Complete   bool           `json:"complete"`
	Members    []OnlineMember `json:"members"`
}

func (s *OnlineSnapshot) Validate() error {
	invalid := errors.New("invalid online session snapshot")
	if s == nil || !s.Complete || s.ObservedAt.IsZero() || s.Members == nil || len(s.Members) > MaxOnlineSnapshotMembers {
		return invalid
	}
	id, err := hex.DecodeString(s.ID)
	if err != nil || len(id) != 16 {
		return invalid
	}
	seen := map[uint16]bool{}
	for _, m := range s.Members {
		if m.CharacterID == 0 || m.Generation == 0 || seen[m.CharacterID] {
			return invalid
		}
		seen[m.CharacterID] = true
	}
	return nil
}

func (c *Client) OnlineSnapshot(ctx context.Context) (*OnlineSnapshot, error) {
	r, err := c.Call(ctx, Request{Action: "online_snapshot"})
	if err != nil {
		return nil, err
	}
	if err = r.OnlineSnapshot.Validate(); err != nil {
		return nil, err
	}
	return r.OnlineSnapshot, nil
}
