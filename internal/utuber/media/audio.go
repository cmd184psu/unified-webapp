package media

import "context"

func ExtractAudio(
	ctx context.Context,
	exec Executor,
	input, output string,
	onProgress func(string),
) error {
	args := []string{
		"-y",
		"-i", input,
		"-vn",
		"-acodec", "libmp3lame",
		"-q:a", "2",
		output,
	}
	// ffmpeg writes to both stdout and stderr; the same callback captures both,
	// preserving the merged behavior this single-callback API had before Run
	// split into per-stream callbacks.
	return exec.Run(ctx, "ffmpeg", args, onProgress, onProgress)
}
