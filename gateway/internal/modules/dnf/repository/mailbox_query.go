package repository

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"
)

// MailboxFilter uses literal substring matching and a half-open creation range.
type MailboxFilter struct {
	Query   string    `json:"q,omitempty"`
	Sender  string    `json:"sender,omitempty"`
	Claimed string    `json:"claimed"`
	From    time.Time `json:"from,omitzero"`
	To      time.Time `json:"to,omitzero"`
}

func (f MailboxFilter) Normalize() (MailboxFilter, error) {
	f.Query = strings.TrimSpace(f.Query)
	f.Sender = strings.TrimSpace(f.Sender)
	if f.Claimed == "" {
		f.Claimed = "all"
	}
	if len(f.Query) > 200 || len(f.Sender) > 200 || !utf8.ValidString(f.Query) || !utf8.ValidString(f.Sender) || strings.ContainsRune(f.Query+f.Sender, 0) || (f.Claimed != "all" && f.Claimed != "claimed" && f.Claimed != "unclaimed") {
		return f, ErrResourceRange
	}
	for _, v := range []time.Time{f.From, f.To} {
		if !v.IsZero() && (v.Year() < 1000 || v.Year() > 9999) {
			return f, ErrResourceRange
		}
	}
	f.From = f.From.UTC()
	f.To = f.To.UTC()
	if !f.From.IsZero() && !f.To.IsZero() && !f.From.Before(f.To) {
		return f, ErrResourceRange
	}
	return f, nil
}

type MailboxFilteredReader interface {
	MailboxFilteredPage(context.Context, string, MailboxFilter, string, int) ([]MailRecord, error)
}
