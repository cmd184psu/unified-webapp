package jobs

type Status string

const (
	Queued    Status = "queued"
	Running   Status = "running"
	Completed Status = "completed"
	Failed    Status = "failed"
)

type Job struct {
	ID           string
	URL          string
	ShowName     string
	EpisodeTitle string
	Season       int
	Episode      int
	Mode         string // "video" or "audio"

	Status     Status
	Progress   string
	OutputFile string
	Error      string
}
