package events

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// FileHandler writes events as JSONL (one JSON line per event) to log files
// under a workspace's logs/ directory. A new log file is created when
// StoryStarted or QAPhaseStarted events are received. Events before the
// first such event go to a startup-<timestamp>.jsonl file.
type FileHandler struct {
	logsDir string
	nowFn   func() time.Time
	file    *os.File
}

// NewFileHandler creates a FileHandler that writes JSONL log files to logsDir.
func NewFileHandler(logsDir string) *FileHandler {
	return &FileHandler{logsDir: logsDir, nowFn: time.Now}
}

func newFileHandler(logsDir string, nowFn func() time.Time) *FileHandler {
	return &FileHandler{logsDir: logsDir, nowFn: nowFn}
}

func (h *FileHandler) Handle(event Event) {
	switch e := event.(type) {
	case StoryStarted:
		h.rotateFile(fmt.Sprintf("%s-%s.jsonl", e.StoryID, h.timestamp()))
	case QAPhaseStarted:
		h.rotateFile(fmt.Sprintf("QA-%s-%s.jsonl", e.Phase, h.timestamp()))
	}

	h.ensureFile()
	h.writeLine(event)
}

// Close closes the current log file.
func (h *FileHandler) Close() {
	if h.file != nil {
		h.file.Close()
		h.file = nil
	}
}

func (h *FileHandler) rotateFile(name string) {
	h.Close()
	f, err := h.createFile(name)
	if err != nil {
		return
	}
	h.file = f
}

func (h *FileHandler) ensureFile() {
	if h.file != nil {
		return
	}
	name := fmt.Sprintf("startup-%s.jsonl", h.timestamp())
	f, err := h.createFile(name)
	if err != nil {
		return
	}
	h.file = f
}

func (h *FileHandler) createFile(name string) (*os.File, error) {
	if err := os.MkdirAll(h.logsDir, 0o755); err != nil {
		return nil, err
	}
	return os.Create(filepath.Join(h.logsDir, name))
}

func (h *FileHandler) writeLine(event Event) {
	if h.file == nil {
		return
	}
	data, err := MarshalEvent(event)
	if err != nil {
		return
	}
	h.file.Write(data)
	h.file.Write([]byte("\n"))
}

func (h *FileHandler) timestamp() string {
	return h.nowFn().UTC().Format("20060102T150405Z")
}
