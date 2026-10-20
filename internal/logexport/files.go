// Package logexport stores explicit, bounded snapshots of the browser's log buffer.
package logexport

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxBytes = 8 << 20

var filenamePattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}-\d{2}-\d{2})(?:-(\d{2,}))?\.txt$`)

type Status struct {
	Available  bool   `json:"available"`
	Directory  string `json:"directory,omitempty"`
	Filename   string `json:"filename,omitempty"`
	ExportedAt string `json:"exportedAt,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

func ValidProjectName(name string) bool {
	return name != "" && name != "." && name != ".." && filepath.Base(name) == name && !strings.ContainsAny(name, "/\\\x00\r\n")
}

func FileTime(name string, location *time.Location) (time.Time, int, bool) {
	parts := filenamePattern.FindStringSubmatch(name)
	if parts == nil {
		return time.Time{}, 0, false
	}
	stamp, err := time.ParseInLocation("2006-01-02-15-04", parts[1], location)
	if err != nil {
		return time.Time{}, 0, false
	}
	sequence := 0
	if parts[2] != "" {
		sequence, err = strconv.Atoi(parts[2])
		if sequence < 2 {
			return time.Time{}, 0, false
		}
	}
	return stamp, sequence, err == nil
}

func directory(root *os.Root, name string, create bool) (*os.Root, error) {
	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) && create {
		if err = root.Mkdir(name, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if err = inheritOwner(root, name); err != nil {
			return nil, err
		}
		info, err = root.Lstat(name)
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("export directory must be a real directory")
	}
	return root.OpenRoot(name)
}

// Run is also used by the network-isolated helper mounted on the project parent.
// os.Root confines all operations, including concurrent symlink replacement.
func Run(base, project, mode, stamp string, createProject bool, input io.Reader) (Status, error) {
	if !ValidProjectName(project) {
		return Status{}, fmt.Errorf("invalid project directory")
	}
	if mode != "read" && mode != "write" {
		return Status{}, fmt.Errorf("invalid export operation")
	}
	var content []byte
	if mode == "write" {
		if t, err := time.Parse("2006-01-02-15-04", stamp); err != nil || t.Format("2006-01-02-15-04") != stamp {
			return Status{}, fmt.Errorf("invalid export timestamp")
		}
		var err error
		content, err = io.ReadAll(io.LimitReader(input, MaxBytes+1))
		if err != nil || len(content) == 0 || len(content) > MaxBytes || !utf8.Valid(content) {
			return Status{}, fmt.Errorf("invalid or oversized log snapshot")
		}
	}
	root, err := os.OpenRoot(base)
	if err != nil {
		return Status{}, err
	}
	defer root.Close()
	projectRoot, err := directory(root, project, mode == "write" && createProject)
	if errors.Is(err, os.ErrNotExist) && createProject && mode == "read" {
		return Status{Available: true}, nil
	}
	if err != nil {
		return Status{}, err
	}
	defer projectRoot.Close()
	logs, err := directory(projectRoot, "logs", mode == "write")
	if errors.Is(err, os.ErrNotExist) && mode == "read" {
		return Status{Available: true}, nil
	}
	if err != nil {
		return Status{}, err
	}
	defer logs.Close()
	if mode == "write" {
		temporary := ".yard-export-" + rand.Text() + ".tmp"
		file, err := logs.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return Status{}, err
		}
		defer logs.Remove(temporary)
		if err = inheritOwner(logs, temporary); err == nil {
			_, err = file.Write(content)
		}
		if err == nil {
			err = file.Sync()
		}
		err = errors.Join(err, file.Close())
		if err != nil {
			return Status{}, err
		}
		for sequence := 1; sequence <= 9999; sequence++ {
			name := stamp + ".txt"
			if sequence > 1 {
				name = fmt.Sprintf("%s-%02d.txt", stamp, sequence)
			}
			// Publish only the complete file, atomically and without overwriting.
			err := logs.Link(temporary, name)
			if errors.Is(err, os.ErrExist) {
				continue
			}
			if err != nil {
				return Status{}, err
			}
			return Status{Available: true, Filename: name}, nil
		}
		return Status{}, fmt.Errorf("too many exports in this minute")
	}
	entries, err := fs.ReadDir(logs.FS(), ".")
	if err != nil {
		return Status{}, err
	}
	result := Status{Available: true}
	var newest time.Time
	var lastSequence int
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		stamp, sequence, valid := FileTime(entry.Name(), time.UTC)
		if valid && (stamp.After(newest) || stamp.Equal(newest) && sequence > lastSequence) {
			result.Filename = entry.Name()
			newest, lastSequence = stamp, sequence
		}
	}
	return result, nil
}
