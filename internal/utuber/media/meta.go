package media

import (
	"context"
	"strings"
)

type Meta struct {
	Uploader string
	Title    string
}

var metaSep = "|||"

func FetchMeta(ctx context.Context, exec Executor, url string) (Meta, error) {
	var output string
	args := []string{
		"--print", "%(uploader)s" + metaSep + "%(title)s",
		"--skip-download",
		"--no-warnings",
		"--extractor-args", "youtube:player_client=android",
		url,
	}
	// yt-dlp --print writes the metadata line to stdout; the same closure feeds
	// both streams so the merged behavior of the pre-split single-callback API
	// is preserved (a metaSep line is captured whichever stream it arrives on).
	capture := func(line string) {
		if strings.Contains(line, metaSep) {
			output = line
		}
	}
	err := exec.Run(ctx, "yt-dlp", args, capture, capture)
	if err != nil {
		return Meta{}, err
	}
	parts := strings.SplitN(output, metaSep, 2)
	if len(parts) == 2 {
		return Meta{Uploader: strings.TrimSpace(parts[0]), Title: strings.TrimSpace(parts[1])}, nil
	}
	return Meta{}, nil
}
