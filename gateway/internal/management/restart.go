package management

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"time"
)

// This client sends only a settings revision over a protected local socket.
// The privileged host helper owns the fixed service name; no shell or command
// is accepted from an HTTP request or configuration.
func gameRestartClient(path string) func(context.Context, uint64) error {
	return func(ctx context.Context, revision uint64) error {
		dialer := net.Dialer{Timeout: 5 * time.Second}
		conn, err := dialer.DialContext(ctx, "unix", path)
		if err != nil {
			return errors.New("restart helper unavailable")
		}
		defer conn.Close()
		deadline := time.Now().Add(22 * time.Second)
		if value, ok := ctx.Deadline(); ok && value.Before(deadline) {
			deadline = value
		}
		if err := conn.SetDeadline(deadline); err != nil {
			return errors.New("restart helper deadline unavailable")
		}
		return exchangeRestart(conn, revision)
	}
}

func exchangeRestart(conn io.ReadWriter, revision uint64) error {
	if err := json.NewEncoder(conn).Encode(struct {
		Revision uint64 `json:"revision"`
	}{revision}); err != nil {
		return errors.New("restart helper write failed")
	}
	var reply struct {
		Accepted bool `json:"accepted"`
	}
	d := json.NewDecoder(io.LimitReader(conn, 1024))
	d.DisallowUnknownFields()
	if d.Decode(&reply) != nil || !reply.Accepted {
		return errors.New("restart helper rejected request")
	}
	return nil
}
