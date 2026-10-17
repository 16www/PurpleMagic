package gmtool

import (
	"dnf90.local/gateway/internal/modules/dnf/repository"
	"encoding/json"
	"time"
)

func v1MailboxFilter(q v1Query) (repository.MailboxFilter, error) {
	f := repository.MailboxFilter{Query: q.Q, Sender: q.Values.Get("sender"), Claimed: q.Values.Get("claimed")}
	var e error
	if raw := q.Values.Get("from"); raw != "" {
		f.From, e = time.Parse(time.RFC3339Nano, raw)
		if e != nil {
			return f, e
		}
	}
	if raw := q.Values.Get("to"); raw != "" {
		f.To, e = time.Parse(time.RFC3339Nano, raw)
		if e != nil {
			return f, e
		}
	}
	return f.Normalize()
}

func v1MailboxFilterIdentity(f repository.MailboxFilter) string {
	raw, _ := json.Marshal(f)
	return string(raw)
}
