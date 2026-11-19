package verify

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// skippedInCopy are the build directories a verify copy leaves out: the
// command regenerates them, and they are stale the moment they are copied. The
// gitfile is left out separately, because a worktree's .git is a file.
var skippedInCopy = map[string]bool{"bin": true, "obj": true}

// CopyWorktree copies src into dst for a verify run that has to write inside
// the tree (verify_in_copy, AF-6): a feedback command verifies a worktree
// bound read-only, and a toolchain such as .NET writes bin/ and obj/ next to
// the sources. dst is replaced. Symlinks are recreated as links and other
// irregular files are skipped.
func CopyWorktree(dst, src string) error {
	if err := os.RemoveAll(dst); err != nil {
		return fmt.Errorf("verify: reset copy %s: %w", dst, err)
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() && skippedInCopy[d.Name()] {
			return fs.SkipDir
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			fi, err := d.Info()
			if err != nil {
				return err
			}
			return os.MkdirAll(target, fi.Mode().Perm())
		case d.Type()&fs.ModeSymlink != 0:
			link, err := os.Readlink(p)
			if err != nil {
				return err
			}
			return os.Symlink(link, target)
		case d.Type().IsRegular():
			return copyFile(p, target)
		default:
			return nil
		}
	})
}

func copyFile(src, dst string) error {
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, fi.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
