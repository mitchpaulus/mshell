package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func TrashFile(absPath string) error {
	// Determine trash directory per FreeDesktop.org Trash spec
	trashDir := os.Getenv("XDG_DATA_HOME")
	if trashDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		trashDir = filepath.Join(home, ".local", "share")
	}
	trashDir = filepath.Join(trashDir, "Trash")

	filesDir := filepath.Join(trashDir, "files")
	infoDir := filepath.Join(trashDir, "info")

	if err := os.MkdirAll(filesDir, 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(infoDir, 0700); err != nil {
		return err
	}

	baseName := filepath.Base(absPath)

	// Find a unique trash name using O_CREATE|O_EXCL for atomicity
	trashName := baseName
	var infoFile *os.File
	for i := 2; ; i++ {
		infoPath := filepath.Join(infoDir, trashName+".trashinfo")
		f, err := os.OpenFile(infoPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			infoFile = f
			break
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		ext := filepath.Ext(baseName)
		stem := baseName[:len(baseName)-len(ext)]
		trashName = fmt.Sprintf("%s.%d%s", stem, i, ext)
	}

	// Write .trashinfo content
	encodedPath := url.PathEscape(absPath)
	// url.PathEscape encodes '/' which we want to keep as literal
	// Re-encode: PathEscape is close but we need to unescape slashes
	// Actually, per the spec we need percent-encoding per RFC 2396.
	// url.PathEscape does the right thing except it encodes '/'.
	// Let's just use a simple approach: encode each component.
	encodedPath = encodeTrashPath(absPath)

	deletionDate := time.Now().Format("2006-01-02T15:04:05")
	content := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n", encodedPath, deletionDate)
	_, err := infoFile.WriteString(content)
	infoFile.Close()
	if err != nil {
		os.Remove(filepath.Join(infoDir, trashName+".trashinfo"))
		return err
	}

	// Move the file into the trash files directory
	destPath := filepath.Join(filesDir, trashName)
	err = os.Rename(absPath, destPath)
	if err != nil {
		// Check for cross-device link error
		var linkErr *os.LinkError
		if errors.As(err, &linkErr) && errors.Is(linkErr.Err, syscall.EXDEV) {
			// Fall back to copy + remove
			if copyErr := CopyFile(absPath, destPath); copyErr != nil {
				os.Remove(filepath.Join(infoDir, trashName+".trashinfo"))
				return copyErr
			}
			if removeErr := os.RemoveAll(absPath); removeErr != nil {
				return removeErr
			}
			return nil
		}
		os.Remove(filepath.Join(infoDir, trashName+".trashinfo"))
		return err
	}

	return nil
}

// encodeTrashPath percent-encodes a path per the FreeDesktop trash spec.
// Slashes are preserved; other non-unreserved characters are encoded.
func encodeTrashPath(path string) string {
	var buf []byte
	for i := 0; i < len(path); i++ {
		c := path[i]
		if c == '/' ||
			(c >= 'A' && c <= 'Z') ||
			(c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '_' || c == '.' || c == '~' {
			buf = append(buf, c)
		} else {
			buf = append(buf, fmt.Sprintf("%%%02X", c)...)
		}
	}
	return string(buf)
}
