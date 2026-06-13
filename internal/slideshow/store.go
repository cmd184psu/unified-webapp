package slideshow

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// imageExts is the set of file extensions treated as images.
var imageExts = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
}

// Subject is the response shape for one entry in GET /items.
type Subject struct {
	Age       int      `json:"age"`
	Timestamp int      `json:"timestamp"`
	Subject   string   `json:"subject"`
	Entries   []string `json:"entries"`
}

// Store scans imageDir for subject subdirectories containing image files.
type Store struct {
	imageDir      string
	ageCutoffDays int
}

// NewStore creates a Store backed by imageDir, creating it if necessary.
// ageCutoffDays filters out subjects whose directory mtime is older than that
// many days; 0 disables the filter.
func NewStore(imageDir string, ageCutoffDays int) (*Store, error) {
	if err := os.MkdirAll(imageDir, 0755); err != nil {
		return nil, err
	}
	return &Store{imageDir: imageDir, ageCutoffDays: ageCutoffDays}, nil
}

// Subjects lists all subject directories and their image file entries, applying
// blacklist (_-prefix) and age cutoff filters.
// Entries are formatted as "{subject}/{filename}" to match the image URL pattern.
func (s *Store) Subjects() ([]Subject, error) {
	dirs, err := os.ReadDir(s.imageDir)
	if err != nil {
		return nil, err
	}

	var cutoff time.Time
	if s.ageCutoffDays > 0 {
		cutoff = time.Now().AddDate(0, 0, -s.ageCutoffDays)
	}

	var subjects []Subject
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		name := d.Name()

		// Blacklist: skip _-prefixed subjects.
		if strings.HasPrefix(name, "_") {
			continue
		}

		// Age filter: skip directories older than cutoff.
		if !cutoff.IsZero() {
			info, err := d.Info()
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				continue
			}
		}

		files, err := os.ReadDir(filepath.Join(s.imageDir, name))
		if err != nil {
			continue
		}
		var entries []string
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			if imageExts[strings.ToLower(filepath.Ext(f.Name()))] {
				entries = append(entries, name+"/"+f.Name())
			}
		}
		if len(entries) == 0 {
			continue
		}
		subjects = append(subjects, Subject{
			Age:       0,
			Timestamp: 0,
			Subject:   name,
			Entries:   entries,
		})
	}
	return subjects, nil
}

// ImagePath returns the validated absolute filesystem path for the requested
// image. It returns an error if the path escapes imageDir or the file extension
// is not an image type.
func (s *Store) ImagePath(subject, item string) (string, error) {
	ext := strings.ToLower(filepath.Ext(item))
	if !imageExts[ext] {
		return "", os.ErrInvalid
	}
	// filepath.Join cleans the path; verify it stays inside imageDir.
	abs := filepath.Join(s.imageDir, subject, item)
	rel, err := filepath.Rel(s.imageDir, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", os.ErrInvalid
	}
	return abs, nil
}
