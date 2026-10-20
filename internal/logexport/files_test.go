package logexport

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSnapshotReadIsNonMutatingAndExportsPreserveFiles(t *testing.T) {
	base := t.TempDir()
	status, err := Run(base, "yard", "read", "", true, nil)
	if err != nil || !status.Available || status.Filename != "" {
		t.Fatal(status, err)
	}
	entries, _ := os.ReadDir(base)
	if len(entries) != 0 {
		t.Fatal("read created project directories")
	}
	for index, value := range []string{"first log\nOlá\n", "second log\n"} {
		status, err = Run(base, "yard", "write", "2026-10-20-11-37", true, strings.NewReader(value))
		if err != nil {
			t.Fatal(err)
		}
		expected := "2026-10-20-11-37.txt"
		if index > 0 {
			expected = "2026-10-20-11-37-02.txt"
		}
		if status.Filename != expected {
			t.Fatal(status)
		}
		content, err := os.ReadFile(filepath.Join(base, "yard", "logs", expected))
		if err != nil || string(content) != value {
			t.Fatal(string(content), err)
		}
	}
	// A larger numeric suffix is newer, even when lexical sorting says otherwise.
	for _, name := range []string{"2026-10-20-11-37-99.txt", "2026-10-20-11-37-100.txt", "2099-99-99-11-37.txt", ".yard-export-partial.tmp"} {
		if err := os.WriteFile(filepath.Join(base, "yard", "logs", name), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	status, err = Run(base, "yard", "read", "", true, nil)
	if err != nil || status.Filename != "2026-10-20-11-37-100.txt" {
		t.Fatal(status, err)
	}
}

func TestSnapshotRejectsPathsAndInvalidContentBeforeCreatingDirectories(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"", ".", "..", "../outside", "one/two", `one\two`, "bad\nname"} {
		if _, err := Run(base, name, "write", "2026-10-20-11-37", true, strings.NewReader("log")); err == nil {
			t.Fatal("accepted", name)
		}
	}
	for _, value := range []string{"", string([]byte{0xff}), strings.Repeat("x", MaxBytes+1)} {
		if _, err := Run(base, "yard", "write", "2026-10-20-11-37", true, strings.NewReader(value)); err == nil {
			t.Fatal("accepted invalid snapshot")
		}
	}
	if _, err := Run(base, "yard", "write", "2026-99-20-11-37", true, strings.NewReader("log")); err == nil {
		t.Fatal("accepted invalid timestamp")
	}
	entries, _ := os.ReadDir(base)
	if len(entries) != 0 {
		t.Fatal("invalid request changed disk")
	}
}

func TestConcurrentExportsPublishUniqueCompleteSnapshots(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "yard", "logs"), 0700); err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for index := range 12 {
		group.Add(1)
		go func() {
			defer group.Done()
			value := fmt.Sprintf("snapshot-%d\n", index)
			status, err := Run(base, "yard", "write", "2026-10-20-11-37", true, strings.NewReader(value))
			if err != nil {
				t.Error(err)
				return
			}
			content, err := os.ReadFile(filepath.Join(base, "yard", "logs", status.Filename))
			if err != nil || string(content) != value {
				t.Error("export was overwritten", err)
			}
		}()
	}
	group.Wait()
	entries, _ := os.ReadDir(filepath.Join(base, "yard", "logs"))
	if len(entries) != 12 {
		t.Fatal("missing or temporary exports", len(entries))
	}
	for _, entry := range entries {
		if _, _, valid := FileTime(entry.Name(), time.UTC); !valid {
			t.Fatal("partial file published", entry.Name())
		}
	}
}

func TestSnapshotRefusesSymbolicLinkDirectoriesAndNeverOverwritesLinks(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "linked")); err != nil {
		t.Skip("symlink creation unavailable:", err)
	}
	if _, err := Run(base, "linked", "write", "2026-10-20-11-37", true, strings.NewReader("log")); err == nil {
		t.Fatal("followed project symlink")
	}
	if err := os.Mkdir(filepath.Join(base, "yard"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(base, "yard", "logs")); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(base, "yard", "write", "2026-10-20-11-37", true, strings.NewReader("log")); err == nil {
		t.Fatal("followed logs symlink")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatal("escaped export directory")
	}
	if err := os.Remove(filepath.Join(base, "yard", "logs")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(base, "yard", "logs"), 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(outside, "keep.txt")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(base, "yard", "logs", "2026-10-20-11-37.txt")); err != nil {
		t.Fatal(err)
	}
	status, err := Run(base, "yard", "write", "2026-10-20-11-37", true, strings.NewReader("new"))
	if err != nil || status.Filename != "2026-10-20-11-37-02.txt" {
		t.Fatal(status, err)
	}
	content, _ := os.ReadFile(target)
	if string(content) != "keep" {
		t.Fatal("overwrote existing symlink target")
	}
}
