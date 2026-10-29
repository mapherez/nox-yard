package inventory

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type LogHistoryQuery struct {
	Since  string `json:"since,omitempty"`
	Until  string `json:"until,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}
type LogPage struct {
	Lines     []LogEvent `json:"lines"`
	Next      string     `json:"next,omitempty"`
	Until     string     `json:"until"`
	Retention string     `json:"retention"`
}
type logCursor struct {
	ID    string            `json:"id"`
	Since string            `json:"since"`
	Until string            `json:"until"`
	Stamp string            `json:"stamp"`
	Seen  map[string]uint64 `json:"seen"`
}

var ErrLogHistoryInput = errors.New("invalid log history request")
var ErrLogTimestamps = errors.New("Docker did not provide timestamps; history pagination is unavailable for this stream")
var errLogPageFull = errors.New("log page complete")

func ReadLogPage(ctx context.Context, reader SnapshotLogReader, id string, q LogHistoryQuery) (LogPage, error) {
	page := LogPage{Lines: []LogEvent{}, Retention: "Only history still available from Docker for this container; rotation and container removal may discard older records."}
	cursor := logCursor{ID: id, Since: q.Since, Until: q.Until, Seen: map[string]uint64{}}
	if q.Cursor != "" {
		if len(q.Cursor) > 4096 {
			return page, fmt.Errorf("%w: invalid cursor", ErrLogHistoryInput)
		}
		data, err := base64.RawURLEncoding.DecodeString(q.Cursor)
		if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.ID != id || cursor.Stamp == "" || len(cursor.Seen) > 3 {
			return page, fmt.Errorf("%w: invalid cursor", ErrLogHistoryInput)
		}
	}
	if cursor.Until == "" {
		cursor.Until = time.Now().UTC().Format(time.RFC3339Nano)
	}
	for _, value := range []string{cursor.Since, cursor.Until, cursor.Stamp} {
		if value != "" {
			if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
				return page, fmt.Errorf("%w: times must be RFC3339 timestamps", ErrLogHistoryInput)
			}
		}
	}
	if cursor.Since != "" {
		a, _ := time.Parse(time.RFC3339Nano, cursor.Since)
		b, _ := time.Parse(time.RFC3339Nano, cursor.Until)
		if a.After(b) {
			return page, fmt.Errorf("%w: interval ends before it starts", ErrLogHistoryInput)
		}
	}
	page.Until = cursor.Until
	since := cursor.Since
	if cursor.Stamp != "" {
		since = cursor.Stamp
	}
	stream, err := reader.OpenLogsWithOptions(ctx, id, LogOptions{Tail: -1, Since: since, Until: cursor.Until})
	if err != nil {
		return page, err
	}
	defer stream.Reader.Close()
	stop := context.AfterFunc(ctx, func() { _ = stream.Reader.Close() })
	defer stop()
	skipped := map[string]uint64{}
	next := cursor
	next.Seen = map[string]uint64{}
	for key, count := range cursor.Seen {
		next.Seen[key] = count
	}
	bytes := 8192
	err = DecodeLogEvents(ctx, stream, func(event LogEvent) error {
		if event.Timestamp == "" {
			return ErrLogTimestamps
		}
		stamp, _ := time.Parse(time.RFC3339Nano, event.Timestamp)
		boundary, _ := time.Parse(time.RFC3339Nano, cursor.Stamp)
		if cursor.Stamp != "" && stamp.Before(boundary) {
			return nil
		}
		if cursor.Stamp != "" && stamp.Equal(boundary) && skipped[event.Stream] < cursor.Seen[event.Stream] {
			skipped[event.Stream]++
			return nil
		}
		encoded, _ := json.Marshal(event)
		if len(page.Lines) >= 1000 || bytes+len(encoded)+1 > MaxLogPageBytes {
			return errLogPageFull
		}
		bytes += len(encoded) + 1
		page.Lines = append(page.Lines, event)
		if next.Stamp != event.Timestamp {
			next.Stamp = event.Timestamp
			next.Seen = map[string]uint64{}
		}
		next.Seen[event.Stream]++
		return nil
	})
	if errors.Is(err, errLogPageFull) {
		encoded, _ := json.Marshal(next)
		page.Next = base64.RawURLEncoding.EncodeToString(encoded)
		return page, nil
	}
	return page, err
}
