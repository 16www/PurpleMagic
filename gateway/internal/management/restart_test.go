package management

import (
	"bytes"
	"strings"
	"testing"
)

type restartExchange struct {
	*bytes.Buffer
	written bytes.Buffer
}

func (x *restartExchange) Write(p []byte) (int, error) { return x.written.Write(p) }
func TestRestartExchangeBoundsAndRejectsUnacknowledgedRequest(t *testing.T) {
	for _, reply := range []string{`{"accepted":false}`, `{"accepted":true,"other":1}`, `not json`, strings.Repeat("x", 1100)} {
		x := &restartExchange{Buffer: bytes.NewBufferString(reply)}
		if exchangeRestart(x, 42) == nil {
			t.Fatal("accepted invalid helper reply")
		}
	}
	x := &restartExchange{Buffer: bytes.NewBufferString(`{"accepted":true}`)}
	if err := exchangeRestart(x, 42); err != nil {
		t.Fatal(err)
	}
	if x.written.String() != "{\"revision\":42}\n" {
		t.Fatal(x.written.String())
	}
}
