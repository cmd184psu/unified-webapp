package smbedit

import (
	"os"
	"path/filepath"
	"strings"
)

// defaultPickerRoot is used when the module config leaves picker_root empty.
// It matches standalone smbed, whose picker was hardwired to /opt.
const defaultPickerRoot = "/opt"

// FolderEntry describes a directory returned by the folder picker.
type FolderEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// ListFolders returns the immediate subdirectories of root (defaulting to
// /opt when root is empty), skipping hidden dirs. It never traverses deeper
// than one level. A missing or unreadable root is an error for the caller to
// surface — the picker root is operator configuration, not user input.
func ListFolders(root string) ([]FolderEntry, error) {
	if root == "" {
		root = defaultPickerRoot
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}

	var folders []FolderEntry
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Skip hidden directories.
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		folders = append(folders, FolderEntry{
			Name: e.Name(),
			Path: filepath.Join(root, e.Name()),
		})
	}
	return folders, nil
}
