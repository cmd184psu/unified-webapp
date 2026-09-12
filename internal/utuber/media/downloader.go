package media

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var progressRegex = regexp.MustCompile(`\[download\]\s+([\d.]+)%`)

func Download(
	ctx context.Context,
	exec Executor,
	url, dir string,
	onProgress func(string),
) (string, error) {

	args := []string{
		"-f", "bestvideo+bestaudio/best",
		"--merge-output-format", "mp4",
		"--no-warnings",
		"--newline",
		"-o", filepath.Join(dir, "%(title)s.%(ext)s"),
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

	return newestFile(dir)
}

func newestFile(dir string) (string, error) {
	var newest string
	var newestTime time.Time

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		info, _ := e.Info()
		if info.ModTime().After(newestTime) {
			newestTime = info.ModTime()
			newest = filepath.Join(dir, e.Name())
		}
	}
	return newest, nil
}
