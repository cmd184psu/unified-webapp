package media

import (
	"context"
	"log"
	"path/filepath"
	"regexp"
	"strings"
)

var progressRegex = regexp.MustCompile(`\[download\]\s+([\d.]+)%`)

// videoFormat selects an Apple-compatible video+audio pair capped at 720p.
//
// QuickTime, the macOS TV.app, and Apple TV decode H.264 (avc1) video and
// AAC (mp4a) audio in an MP4 container; they cannot play the VP9/AV1 video or
// Opus audio that yt-dlp's old "bestvideo+bestaudio/best" default happily
// grabbed. That default also pulled the largest rendition available (up to
// 4K), which is why the output was both unplayable on Apple devices and far
// larger than expected.
//
// The selector prefers avc1 video + stereo mp4a audio at ≤720p (a clean
// stream copy on YouTube, which always offers this pair up to 1080p), then
// falls back progressively so a source lacking that exact combination still
// downloads.
const videoFormat = "bv*[height<=720][vcodec^=avc1]+ba[acodec^=mp4a][audio_channels<=2]/" +
	"b[height<=720][ext=mp4]/" +
	"bv*[height<=720]+ba/" +
	"b[height<=720]/b"

// videoFormatSort breaks ties within videoFormat toward the same
// Apple-friendly choices: 720p, H.264, AAC, stereo.
const videoFormatSort = "res:720,vcodec:h264,acodec:aac,channels:2"

// Download fetches url into dir, writing a single MP4 named stem+".mp4", and
// returns that path.
//
// The output name is deterministic on purpose. yt-dlp is told to write
// "<stem>.%(ext)s"; --merge-output-format mp4 and --remux-video mp4 together
// guarantee the container (and therefore the extension) is always mp4, so the
// finished file is exactly "<stem>.mp4" — no directory globbing to guess which
// file yt-dlp produced. The old approach scanned dir for the newest-mtime
// entry, which could latch onto a transient post-processing file or a leftover
// download and rename that instead of the real video. The caller passes a
// unique stem (the job ID) and renames the result to its final name.
func Download(
	ctx context.Context,
	exec Executor,
	url, dir, stem string,
	onProgress func(string),
) (string, error) {

	outPath := filepath.Join(dir, stem+".mp4")
	args := []string{
		"-f", videoFormat,
		"-S", videoFormatSort,
		"--merge-output-format", "mp4",
		// Force the final container to mp4 even when a fallback selector picks
		// a single non-mp4 stream, so the returned "<stem>.mp4" path is always
		// the file yt-dlp actually wrote.
		"--remux-video", "mp4",
		// Retain subtitles as selectable soft tracks (mov_text), never burned
		// into the picture. --embed-subs downloads the manual subtitle tracks,
		// muxes them in, and deletes the sidecar files afterward.
		"--embed-subs",
		"--sub-langs", "all",
		"--embed-metadata",
		"--no-warnings",
		"--newline",
		"-o", filepath.Join(dir, stem+".%(ext)s"),
		url,
	}
	progressCallback := func(line string) {
		if matches := progressRegex.FindStringSubmatch(line); len(matches) > 1 {
			onProgress(matches[1]) // Sends "45.2", "100.0"
		}
	}

	log.Printf("downloading using cli: %s\n", "yt-dlp "+strings.Join(args, " "))
	if err := exec.Run(ctx, "yt-dlp", args, progressCallback); err != nil {
		return "", err
	}

	return outPath, nil
}
