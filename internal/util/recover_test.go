package util

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestRecoverLog_SwallowsPanicAndLogsContext(t *testing.T) {
	var buf bytes.Buffer
	ctx := zerolog.New(&buf).WithContext(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		defer RecoverLog(ctx, "unit test goroutine")
		panic("boom")
	}()
	<-done

	out := buf.String()
	for _, want := range []string{`"panic":"boom"`, "unit test goroutine", `"stack":`} {
		if !strings.Contains(out, want) {
			t.Fatalf("log %q lacks %q", out, want)
		}
	}
}

func TestRecoverLog_NoPanicLogsNothing(t *testing.T) {
	var buf bytes.Buffer
	ctx := zerolog.New(&buf).WithContext(context.Background())

	func() {
		defer RecoverLog(ctx, "quiet")
	}()

	if buf.Len() != 0 {
		t.Fatalf("expected no log line, got %q", buf.String())
	}
}
