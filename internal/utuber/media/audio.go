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
	return exec.Run(ctx, "ffmpeg", args, onProgress)
}
