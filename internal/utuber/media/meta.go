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
	err := exec.Run(ctx, "yt-dlp", args, func(line string) {
		if strings.Contains(line, metaSep) {
			output = line
		}
	})
	if err != nil {
		return Meta{}, err
	}
	parts := strings.SplitN(output, metaSep, 2)
	if len(parts) == 2 {
		return Meta{Uploader: strings.TrimSpace(parts[0]), Title: strings.TrimSpace(parts[1])}, nil
	}
	return Meta{}, nil
}
