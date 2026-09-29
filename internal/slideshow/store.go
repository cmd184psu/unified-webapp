package slideshow

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"cmd184psu/unified-webapp/internal/platform/fspath"
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
// ageCutoffDays is the default image age limit (see SubjectsWithin); 0
// means no limit.
func NewStore(imageDir string, ageCutoffDays int) (*Store, error) {
	if err := os.MkdirAll(imageDir, 0750); err != nil {
		return nil, err
	}
	return &Store{imageDir: imageDir, ageCutoffDays: ageCutoffDays}, nil
}

// DefaultMaxAgeDays is the configured starting value for the image age limit.
func (s *Store) DefaultMaxAgeDays() int { return s.ageCutoffDays }

// Subjects is SubjectsWithin the configured default age limit.
func (s *Store) Subjects() ([]Subject, error) { return s.SubjectsWithin(s.ageCutoffDays) }

// SubjectsWithin lists all subject directories and their image entries,
// skipping _-prefixed subjects and any image whose file was last modified
// more than maxAgeDays days ago (0 = no age limit). A subject left with no
// images is skipped entirely. Entries are "{subject}/{filename}" to match
// the image URL pattern.
func (s *Store) SubjectsWithin(maxAgeDays int) ([]Subject, error) {
	dirs, err := os.ReadDir(s.imageDir)
	if err != nil {
		return nil, err
	}

	var cutoff time.Time
	if maxAgeDays > 0 {
		cutoff = time.Now().AddDate(0, 0, -maxAgeDays)
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

		files, err := os.ReadDir(filepath.Join(s.imageDir, name))
		if err != nil {
			continue
		}
		var entries []string
		for _, f := range files {
			if f.IsDir() || !imageExts[strings.ToLower(filepath.Ext(f.Name()))] {
				continue
			}
			// Age limit: by each image's own modified time.
			if !cutoff.IsZero() {
				info, err := f.Info()
				if err != nil || info.ModTime().Before(cutoff) {
					continue
				}
			}
			entries = append(entries, name+"/"+f.Name())
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
	abs, err := fspath.ConfineTo(s.imageDir, filepath.Join(subject, item))
	if err != nil {
		return "", os.ErrInvalid
	}
	return abs, nil
}
