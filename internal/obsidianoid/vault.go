package obsidianoid

import (
	"bytes"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"

	"cmd184psu/unified-webapp/internal/platform/fspath"
)

type noteEntry struct {
	Path, Name, AbsPath string
	Mtime               int64 // Unix milliseconds
}

func vaultList(root string) ([]noteEntry, error) {
	var notes []noteEntry
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.ToLower(filepath.Ext(d.Name())) != ".md" {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		var mtime int64
		if info, err := d.Info(); err == nil {
			mtime = info.ModTime().UnixMilli()
		}
		notes = append(notes, noteEntry{
			Path:    rel,
			Name:    strings.TrimSuffix(d.Name(), filepath.Ext(d.Name())),
			AbsPath: path,
			Mtime:   mtime,
		})
		return nil
	})
	return notes, err
}

// VaultTree is exported for tests.
func VaultTree(root string) (*TreeNode, error) { return vaultTree(root) }

// ReadNote is exported for tests.
func ReadNote(root, relPath string) ([]byte, error) { return readNote(root, relPath) }

// WriteNote is exported for tests.
func WriteNote(root, relPath string, content []byte) error { return writeNote(root, relPath, content) }

func vaultTree(root string) (*TreeNode, error) {
	notes, err := vaultList(root)
	if err != nil {
		return nil, err
	}
	rootNode := &TreeNode{Name: filepath.Base(root), IsDir: true}
	for _, n := range notes {
		treeInsert(rootNode, strings.Split(n.Path, "/"), n.Path, n.Mtime)
	}
	// Folders with no notes yet (a freshly created one) still belong in the
	// tree, or there would be nothing to drag a note into.
	dirs, err := vaultDirs(root)
	if err != nil {
		return nil, err
	}
	for _, d := range dirs {
		ensureDir(rootNode, strings.Split(d, "/"), d)
	}
	return rootNode, nil
}

// vaultDirs lists every non-hidden folder in the vault, vault-relative.
func vaultDirs(root string) ([]string, error) {
	var dirs []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() || p == root {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		rel, _ := filepath.Rel(root, p)
		dirs = append(dirs, filepath.ToSlash(rel))
		return nil
	})
	return dirs, err
}

// ensureDir makes sure the folder chain parts exists under parent, creating
// empty folder nodes as needed. Folders carry their vault-relative Path so the
// UI can name them as drop targets.
func ensureDir(parent *TreeNode, parts []string, fullPath string) {
	if len(parts) == 0 {
		return
	}
	var dir *TreeNode
	for _, child := range parent.Children {
		if child.IsDir && child.Name == parts[0] {
			dir = child
			break
		}
	}
	if dir == nil {
		dir = &TreeNode{Name: parts[0], IsDir: true}
		parent.Children = append(parent.Children, dir)
	}
	if len(parts) == 1 {
		dir.Path = fullPath
		return
	}
	ensureDir(dir, parts[1:], fullPath)
}

// validFolder reports whether rel is a safe vault-relative folder path: one or
// more valid names separated by "/". The empty string is the vault root.
func validFolder(rel string) bool {
	if rel == "" {
		return true
	}
	for _, part := range strings.Split(rel, "/") {
		if !fspath.ValidName(part) {
			return false
		}
	}
	return true
}

// createFolder makes a new (possibly nested) folder in the vault.
func createFolder(root, rel string) error {
	rel = strings.Trim(strings.TrimSpace(rel), "/")
	if rel == "" || !validFolder(rel) {
		return errInvalidName
	}
	dst, err := fspath.ConfineTo(root, filepath.FromSlash(rel))
	if err != nil {
		return os.ErrPermission
	}
	if _, err := os.Stat(dst); err == nil {
		return errNoteExists
	}
	return os.MkdirAll(dst, 0o750)
}

var errFolderNotEmpty = errors.New("folder is not empty")

// renameFolder gives the folder at rel a new name in the same parent and
// returns its new vault-relative path. An existing folder or note by that
// name is never overwritten.
func renameFolder(root, rel, newName string) (string, error) {
	rel = strings.Trim(strings.TrimSpace(rel), "/")
	newName = strings.TrimSpace(newName)
	if rel == "" || !validFolder(rel) || !fspath.ValidName(newName) {
		return "", errInvalidName
	}
	src, err := fspath.ConfineTo(root, filepath.FromSlash(rel))
	if err != nil {
		return "", os.ErrPermission
	}
	info, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", os.ErrNotExist
	}
	newRel := path.Join(path.Dir(rel), newName)
	if path.Dir(rel) == "." {
		newRel = newName
	}
	if newRel == rel {
		return newRel, nil
	}
	dst, err := fspath.ConfineTo(root, filepath.FromSlash(newRel))
	if err != nil {
		return "", os.ErrPermission
	}
	// A case-only rename is the same folder on a case-insensitive disk.
	if dstInfo, err := os.Stat(dst); err == nil && !os.SameFile(info, dstInfo) {
		return "", errNoteExists
	}
	if err := os.Rename(src, dst); err != nil {
		return "", err
	}
	return newRel, nil
}

// removeFolder deletes an empty folder. Anything inside it (including hidden
// files) makes it non-empty, and it is left alone; the vault root is refused.
func removeFolder(root, rel string) error {
	rel = strings.Trim(strings.TrimSpace(rel), "/")
	if rel == "" || !validFolder(rel) {
		return errInvalidName
	}
	dir, err := fspath.ConfineTo(root, filepath.FromSlash(rel))
	if err != nil {
		return os.ErrPermission
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return os.ErrNotExist
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return errFolderNotEmpty
	}
	return os.Remove(dir)
}

// moveNote moves the note at relPath into folder ("" = vault root), keeping
// its file name, and returns the new vault-relative path. The folder must
// already exist; an existing note there is replaced only when overwrite is set.
func moveNote(root, relPath, folder string, overwrite bool) (string, error) {
	folder = strings.Trim(strings.TrimSpace(folder), "/")
	if !validFolder(folder) {
		return "", errInvalidName
	}
	src, err := fspath.ConfineTo(root, filepath.FromSlash(relPath))
	if err != nil {
		return "", os.ErrPermission
	}
	if _, err := os.Stat(src); err != nil {
		return "", err
	}
	dstDir, err := fspath.ConfineTo(root, filepath.FromSlash(folder))
	if err != nil {
		return "", os.ErrPermission
	}
	if info, err := os.Stat(dstDir); err != nil || !info.IsDir() {
		return "", os.ErrNotExist
	}
	newRel := path.Join(folder, path.Base(relPath))
	if newRel == relPath {
		return newRel, nil
	}
	dst := filepath.Join(dstDir, filepath.Base(src))
	if dstInfo, err := os.Stat(dst); err == nil && (!overwrite || dstInfo.IsDir()) {
		return "", errNoteExists
	}
	if err := os.Rename(src, dst); err != nil {
		return "", err
	}
	return newRel, nil
}

// treeInsert places one note, raising each ancestor folder's Mtime to the
// newest note beneath it on the way down.
func treeInsert(parent *TreeNode, parts []string, fullPath string, mtime int64) {
	if len(parts) == 0 {
		return
	}
	if mtime > parent.Mtime {
		parent.Mtime = mtime
	}
	if len(parts) == 1 {
		parent.Children = append(parent.Children, &TreeNode{
			Name:  strings.TrimSuffix(parts[0], ".md"),
			Path:  fullPath,
			Mtime: mtime,
		})
		return
	}
	for _, child := range parent.Children {
		if child.IsDir && child.Name == parts[0] {
			treeInsert(child, parts[1:], fullPath, mtime)
			return
		}
	}
	dir := &TreeNode{Name: parts[0], IsDir: true}
	parent.Children = append(parent.Children, dir)
	treeInsert(dir, parts[1:], fullPath, mtime)
}

// maxSearchQuery bounds a search string; a grep for more than this is a paste
// accident, not a search.
const maxSearchQuery = 200

// vaultSearch returns the vault-relative paths of every note whose name or
// content contains query, case-insensitively — a grep over the vault.
func vaultSearch(root, query string) ([]string, error) {
	q := bytes.ToLower([]byte(query))
	notes, err := vaultList(root)
	if err != nil {
		return nil, err
	}
	matches := []string{}
	for _, n := range notes {
		if bytes.Contains(bytes.ToLower([]byte(n.Name)), q) {
			matches = append(matches, n.Path)
			continue
		}
		content, err := os.ReadFile(n.AbsPath)
		if err != nil {
			continue
		}
		if bytes.Contains(bytes.ToLower(content), q) {
			matches = append(matches, n.Path)
		}
	}
	return matches, nil
}

// deleteNote removes the note at relPath. Only markdown notes inside the
// vault can be deleted; folders and other files are refused.
func deleteNote(root, relPath string) error {
	if strings.ToLower(filepath.Ext(relPath)) != ".md" {
		return os.ErrPermission
	}
	clean, err := fspath.ConfineTo(root, filepath.FromSlash(relPath))
	if err != nil {
		return os.ErrPermission
	}
	info, err := os.Stat(clean)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return os.ErrPermission
	}
	return os.Remove(clean)
}

var (
	errInvalidName = errors.New("invalid note name")
	errNoteExists  = errors.New("a note with that name already exists")
)

// renameNote gives the note at relPath a new name in the same folder and
// returns its new vault-relative path. The ".md" extension is kept (and added
// if newName omits it); an existing note is replaced only when overwrite is set.
func renameNote(root, relPath, newName string, overwrite bool) (string, error) {
	newName = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(newName), ".md"))
	if !fspath.ValidName(newName) {
		return "", errInvalidName
	}
	src, err := fspath.ConfineTo(root, filepath.FromSlash(relPath))
	if err != nil {
		return "", os.ErrPermission
	}
	if _, err := os.Stat(src); err != nil {
		return "", err
	}
	newRel := filepath.ToSlash(filepath.Join(filepath.Dir(filepath.FromSlash(relPath)), newName+".md"))
	dst, err := fspath.ConfineTo(root, filepath.FromSlash(newRel))
	if err != nil {
		return "", os.ErrPermission
	}
	if dst == src {
		return newRel, nil
	}
	// A case-only rename on a case-insensitive filesystem stats as existing
	// (it is the same file), so only refuse when the target is a different file.
	if dstInfo, err := os.Stat(dst); err == nil {
		srcInfo, _ := os.Stat(src)
		if !os.SameFile(srcInfo, dstInfo) && (!overwrite || dstInfo.IsDir()) {
			return "", errNoteExists
		}
	}
	if err := os.Rename(src, dst); err != nil {
		return "", err
	}
	return newRel, nil
}

func readNote(root, relPath string) ([]byte, error) {
	clean, err := fspath.ConfineTo(root, filepath.FromSlash(relPath))
	if err != nil {
		return nil, os.ErrPermission
	}
	return os.ReadFile(clean)
}

func writeNote(root, relPath string, content []byte) error {
	clean, err := fspath.ConfineTo(root, filepath.FromSlash(relPath))
	if err != nil {
		return os.ErrPermission
	}
	if err := os.MkdirAll(filepath.Dir(clean), 0o750); err != nil {
		return err
	}
	return os.WriteFile(clean, content, 0o600)
}
