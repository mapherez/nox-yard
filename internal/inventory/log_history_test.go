package inventory

import (
	"context"
	"io"
	"strconv"
	"strings"
	"testing"
)

type historyReader struct {
	data    string
	options []LogOptions
}

func (r *historyReader) OpenLogsWithOptions(_ context.Context, _ string, o LogOptions) (LogStream, error) {
	r.options = append(r.options, o)
	return LogStream{TTY: true, Reader: io.NopCloser(strings.NewReader(r.data))}, nil
}

func TestHistoryPaginationPreservesIdenticalTimestampMultiplicity(t *testing.T) {
	const stamp = "2026-10-28T10:00:00.000000000Z"
	r := &historyReader{}
	for i := 0; i < 1003; i++ {
		r.data += stamp + " repeat-" + strconv.Itoa(i) + "\n"
	}
	first, err := ReadLogPage(t.Context(), r, "container", LogHistoryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Lines) != 1000 || first.Next == "" {
		t.Fatal("first page not bounded", len(first.Lines))
	}
	second, err := ReadLogPage(t.Context(), r, "container", LogHistoryQuery{Cursor: first.Next})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Lines) != 3 || second.Next != "" || !strings.HasSuffix(second.Lines[0].Text, "repeat-1000") {
		t.Fatal("same timestamp occurrences lost or repeated", second)
	}
	if r.options[1].Since != stamp || r.options[0].Until != r.options[1].Until {
		t.Fatal("history window changed between pages", r.options)
	}
	if _, err := ReadLogPage(t.Context(), r, "other", LogHistoryQuery{Cursor: first.Next}); err == nil {
		t.Fatal("cursor reused across containers")
	}
}

func TestLogFragmentsPreserveUnicodeEmptyLinesAndRealBoundaries(t *testing.T) {
	text := strings.Repeat("á", MaxLogLineBytes) + "\n\nlast"
	raw := LogStream{TTY: true, Reader: io.NopCloser(strings.NewReader(text))}
	var reconstructed strings.Builder
	events := 0
	err := DecodeLogEvents(t.Context(), raw, func(e LogEvent) error {
		events++
		if e.Stream != "combined" {
			t.Fatal("TTY stream claimed a distinct output channel")
		}
		reconstructed.WriteString(e.Text)
		if e.LineEnd {
			reconstructed.WriteByte('\n')
		}
		return nil
	})
	if err != nil || reconstructed.String() != text || events < 4 {
		t.Fatal("log content or boundaries changed", err, events)
	}
}

func TestHistoryRejectsInvalidIntervalAndCursor(t *testing.T) {
	r := &historyReader{}
	for _, q := range []LogHistoryQuery{{Cursor: "invalid"}, {Since: "yesterday"}, {Since: "2026-10-29T00:00:00Z", Until: "2026-10-28T00:00:00Z"}} {
		if _, err := ReadLogPage(t.Context(), r, "container", q); err == nil {
			t.Fatal("invalid history input accepted", q)
		}
	}
}
