package repository

import (
	"context"
	"time"
)

type MailboxCleanupItem struct {
	Ordinal int64  `json:"ordinal"`
	Payload []byte `json:"-"`
}

// Used only inside WithinMailboxManagement; pages and snapshots are bounded.
type MailboxCleanupStore interface {
	MailboxVersion() time.Time
	CleanupCandidates(context.Context, MailboxFilter, string, int) ([]MailRecord, error)
	LoadCleanup(context.Context, string) ([]byte, bool, error)
	SaveCleanup(context.Context, string, []byte) error
	AppendCleanupItems(context.Context, string, []MailboxCleanupItem) error
	CleanupItems(context.Context, string, int64, int) ([]MailboxCleanupItem, error)
}

type MailboxCleanupReader interface {
	MailboxCleanupLog(context.Context, string, int64, int) ([]ManagementLogRow, error)
}
