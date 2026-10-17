package repository

import (
	"context"
	"errors"
	"time"
)

var ErrManagedMailMissing = errors.New("managed mail missing or deleted")

type MailboxManagementState struct {
	UpdatedAt time.Time
	Mails     map[string]MailRecord
}
type MailboxManagementRepository interface {
	LoadManagedMails(context.Context, []string) (MailboxManagementState, error)
	DeleteManagedMails(context.Context, []string, time.Time) error
}
type MailboxManagementUnitOfWork interface {
	// Locks the mailbox parent used by gameplay claim, then validates its owner.
	WithinMailboxManagement(context.Context, string, string, func(MailboxManagementRepository, ManagementReceiptStore) error) error
	MailboxManagementLog(context.Context, string, int64, int) ([]ManagementLogRow, error)
}
