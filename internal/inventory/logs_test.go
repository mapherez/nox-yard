package inventory

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"
)

func frame(stream byte, text string) []byte {
	b := make([]byte, 8)
	b[0] = stream
	binary.BigEndian.PutUint32(b[4:], uint32(len(text)))
	return append(b, []byte(text)...)
}
func TestSharedLogDecoding(t *testing.T) {
	tests := []struct {
		name string
		tty  bool
		data []byte
		want []LogLine
	}{
		{"empty", true, nil, nil},
		{"tty", true, []byte("one\r\ntwo"), []LogLine{{"combined", "one"}, {"combined", "two"}}},
		{"multiplexed", false, append(frame(1, "out\n"), frame(2, "err\n")...), []LogLine{{"stdout", "out"}, {"stderr", "err"}}},
		{"long line", true, []byte(strings.Repeat("x", MaxLogLineBytes+1)), []LogLine{{"combined", strings.Repeat("x", MaxLogLineBytes+1)}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []LogLine
			err := DecodeLogs(context.Background(), LogStream{TTY: tt.tty, Reader: io.NopCloser(bytes.NewReader(tt.data))}, func(line LogLine) error { got = append(got, line); return nil })
			if err != nil || len(got) != len(tt.want) {
				t.Fatalf("%v: %+v", err, got)
			}
			for i, line := range got {
				if line != tt.want[i] {
					t.Fatalf("line %d: %+v", i, line)
				}
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := DecodeLogs(ctx, LogStream{TTY: true, Reader: io.NopCloser(strings.NewReader("line\n"))}, func(LogLine) error { return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestInvalidLogBytesRemainBounded(t *testing.T) {
	data := bytes.Repeat([]byte{0xff}, MaxLogLineBytes*3)
	count := 0
	err := DecodeLogEvents(context.Background(), LogStream{TTY: true, Reader: io.NopCloser(bytes.NewReader(data))}, func(event LogEvent) error {
		count++
		if len(event.Text) > MaxLogLineBytes+4 {
			t.Fatalf("unbounded event: %d", len(event.Text))
		}
		return nil
	})
	if err != nil || count < 3 {
		t.Fatalf("%v: %d events", err, count)
	}
}
