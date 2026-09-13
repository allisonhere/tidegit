package git

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ReflogEntry is one HEAD reflog record, parsed into the facts a recovery view
// needs rather than Git's raw line. Selector is normalised to the familiar
// HEAD@{n} form; the timestamp comes from the reflog entry itself, not the
// commit it points at.
type ReflogEntry struct {
	Selector string
	OID      string
	Short    string
	Action   string // commit, reset, checkout, rebase (…) …
	Detail   string // the rest of Git's reflog subject
	Subject  string // the commit subject
	Actor    string // the identity that performed the action
	Time     time.Time
}

// reflogFormat and its field order must stay in step with parseReflog.
const reflogFormat = "%gd" + fieldSep + "%H" + fieldSep + "%h" + fieldSep +
	"%gs" + fieldSep + "%s" + fieldSep + "%gn" + recordSep

// ReflogBatch is the default number of entries loaded.
const ReflogBatch = 200

// Reflog lists HEAD's reflog, newest first. --date=unix makes the selector
// carry the reflog entry's own timestamp, so the relative time reflects when
// the action happened even when it produced no new commit.
func (r Repository) Reflog(ctx context.Context, limit int) ([]ReflogEntry, error) {
	if limit <= 0 {
		limit = ReflogBatch
	}
	res, err := run(ctx, r.Root, "reflog", "--date=unix", "-n", strconv.Itoa(limit),
		"--format="+reflogFormat)
	if err != nil {
		if isUnbornRevision(err) {
			return nil, nil
		}
		var ce *CommandError
		if errors.As(err, &ce) && strings.Contains(ce.Result.Stderr, "unknown revision") {
			return nil, nil
		}
		return nil, err
	}
	if res.Truncated {
		return nil, fmt.Errorf("reflog exceeds 4 MiB")
	}
	return parseReflog(res.Stdout)
}

func parseReflog(raw string) ([]ReflogEntry, error) {
	var entries []ReflogEntry
	index := 0
	for _, record := range strings.Split(raw, recordSep) {
		record = strings.TrimLeft(record, "\n")
		if strings.TrimSpace(record) == "" {
			continue
		}
		f := strings.Split(record, fieldSep)
		if len(f) != 6 {
			return nil, fmt.Errorf("malformed reflog record with %d fields", len(f))
		}
		entry := ReflogEntry{OID: f[1], Short: f[2], Subject: f[4], Actor: f[5],
			Selector: fmt.Sprintf("HEAD@{%d}", index)}
		entry.Action, entry.Detail, _ = strings.Cut(f[3], ": ")
		if when, ok := parseSelectorTime(f[0]); ok {
			entry.Time = when
		}
		entries = append(entries, entry)
		index++
	}
	return entries, nil
}

// parseSelectorTime reads the Unix timestamp Git writes inside the selector
// when --date=unix is in effect.
func parseSelectorTime(selector string) (time.Time, bool) {
	open := strings.LastIndexByte(selector, '{')
	close := strings.LastIndexByte(selector, '}')
	if open < 0 || close <= open {
		return time.Time{}, false
	}
	n, err := strconv.ParseInt(selector[open+1:close], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(n, 0), true
}
