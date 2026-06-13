package slideshow

import (
	"os"
	"path/filepath"
	"strings"
)

// audioExts is the set of file extensions treated as audio.
var audioExts = map[string]bool{
	".mp3":  true,
	".flac": true,
	".ogg":  true,
	".m4a":  true,
	".wav":  true,
	".aac":  true,
}

// MusicInfo describes one audio collection (one subdirectory of audio_dir).
type MusicInfo struct {
	Name   string   `json:"name"`
	Tracks []string `json:"tracks"` // "CollectionName/filename.mp3"
}

// MusicStore scans audioDir for collection subdirectories.
// Each non-hidden, non-blacklisted subdirectory is one collection; audio files
// within it are the tracks, returned in filesystem sort order.
type MusicStore struct {
	audioDir    string
	collections []MusicInfo
}

// NewMusicStore scans audioDir and returns a ready store.
// If audioDir is empty or unreadable, the store is empty (music disabled).
func NewMusicStore(audioDir string) *MusicStore {
	ms := &MusicStore{audioDir: audioDir}
	if audioDir != "" {
		ms.scan()
	}
	return ms
}

func (ms *MusicStore) scan() {
	dirs, err := os.ReadDir(ms.audioDir)
	if err != nil {
		return
	}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		name := d.Name()
		if strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		files, err := os.ReadDir(filepath.Join(ms.audioDir, name))
		if err != nil {
			continue
		}
		var tracks []string
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			if audioExts[strings.ToLower(filepath.Ext(f.Name()))] {
				tracks = append(tracks, name+"/"+f.Name())
			}
		}
		if len(tracks) == 0 {
			continue
		}
		ms.collections = append(ms.collections, MusicInfo{Name: name, Tracks: tracks})
	}
}

// Collections returns all discovered audio collections.
func (ms *MusicStore) Collections() []MusicInfo {
	return ms.collections
}

// AudioPath returns the validated absolute filesystem path for a requested
// audio file. Returns an error if the path escapes audioDir or the extension
// is not an audio type.
func (ms *MusicStore) AudioPath(collection, track string) (string, error) {
	if ms.audioDir == "" {
		return "", os.ErrNotExist
	}
	ext := strings.ToLower(filepath.Ext(track))
	if !audioExts[ext] {
		return "", os.ErrInvalid
	}
	abs := filepath.Join(ms.audioDir, collection, track)
	rel, err := filepath.Rel(ms.audioDir, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", os.ErrInvalid
	}
	return abs, nil
}
