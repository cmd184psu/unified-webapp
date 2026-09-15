package timetracker

import "errors"

// Customer is a single tracked customer/project entry. The legacy
// remote-access field is deliberately absent (FR-F6): that feature is removed.
type Customer struct {
	CustomerName   string `json:"customerName"`
	SlackChannel   string `json:"slackChannel"`
	SlackChannelId string `json:"slackChannelId"`
	InsightUrl     string `json:"insightUrl"`
	WorkLoadType   string `json:"workLoadType"`
	SfdcUrl        string `json:"sfdcUrl"`
	CumulusBucket  string `json:"cumulusBucket"`
	Jira           string `json:"jira"`
}

// Data is the on-disk JSON format and the shape returned by the API.
type Data struct {
	CompanyName string     `json:"companyName"`
	ProjectName string     `json:"projectName"`
	Author      string     `json:"author"`
	Version     string     `json:"version"`
	Customers   []Customer `json:"customers"`
}

// Sentinel errors for store mutations. The handler discriminates on these to
// return 400 (FR-F2, FR-F5) rather than panicking or silently no-oping.
var (
	ErrInvalidIndex = errors.New("invalid index")
	ErrInvalidField = errors.New("invalid field")
)
