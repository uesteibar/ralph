package events

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LogReader reads JSONL log files from a directory and delivers parsed events
// via a channel. It supports tailing: detecting new files and new lines
// appended to existing files.
type LogReader struct {
	logsDir string
	ch      chan Event
	offsets map[string]int64
}

// NewLogReader creates a LogReader that reads from the given logs directory.
func NewLogReader(logsDir string) *LogReader {
	return &LogReader{
		logsDir: logsDir,
		ch:      make(chan Event, 64),
		offsets: make(map[string]int64),
	}
}

// Events returns the channel on which parsed events are delivered.
func (r *LogReader) Events() <-chan Event {
	return r.ch
}

// Run reads existing log files and then polls for new content until ctx is
// cancelled. It closes the events channel when it returns.
func (r *LogReader) Run(ctx context.Context) {
	defer close(r.ch)

	for {
		r.readNewEntries(ctx)

		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// readNewEntries scans all .jsonl files for new lines since the last read.
func (r *LogReader) readNewEntries(ctx context.Context) {
	files, err := filepath.Glob(filepath.Join(r.logsDir, "*.jsonl"))
	if err != nil || len(files) == 0 {
		return
	}
	sortByModTime(files)

	for _, path := range files {
		offset := r.offsets[path]
		f, err := os.Open(path)
		if err != nil {
			continue
		}

		if offset > 0 {
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				f.Close()
				continue
			}
		}

		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			evt, err := UnmarshalEvent([]byte(line))
			if err != nil {
				continue
			}

			select {
			case r.ch <- evt:
			case <-ctx.Done():
				f.Close()
				return
			}
		}

		newOffset, _ := f.Seek(0, io.SeekCurrent)
		r.offsets[path] = newOffset
		f.Close()
	}
}

// sortByModTime sorts file paths by modification time (oldest first).
// Falls back to alphabetical order for files that cannot be stat'd.
func sortByModTime(files []string) {
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	infos := make([]fileInfo, len(files))
	for i, path := range files {
		info, err := os.Stat(path)
		if err != nil {
			infos[i] = fileInfo{path: path}
		} else {
			infos[i] = fileInfo{path: path, modTime: info.ModTime()}
		}
	}
	sort.Slice(infos, func(i, j int) bool {
		if infos[i].modTime.Equal(infos[j].modTime) {
			return infos[i].path < infos[j].path
		}
		return infos[i].modTime.Before(infos[j].modTime)
	})
	for i, info := range infos {
		files[i] = info.path
	}
}
