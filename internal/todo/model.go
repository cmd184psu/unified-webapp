package todo

// ColumnVisibility controls which optional columns are shown in the UI.
type ColumnVisibility struct {
	Votes    bool `json:"votes"`
	Period   bool `json:"period"`
	NextDue  bool `json:"next_due"`
	Cooldown bool `json:"cooldown"`
}

func defaultColumns() ColumnVisibility {
	return ColumnVisibility{Votes: true, Period: true, NextDue: true, Cooldown: true}
}

// Settings holds general UI preferences for the todo module.
type Settings struct {
	CooldownMinutes int `json:"cooldown_minutes"`
}

func defaultSettings() Settings {
	return Settings{CooldownMinutes: 10}
}

// Subject is the response shape for GET /items.
type Subject struct {
	Age       int      `json:"age"`
	Timestamp int      `json:"timestamp"`
	Subject   string   `json:"subject"`
	Entries   []string `json:"entries"`
}

// IndexItem is one entry in a generated index list.
type IndexItem struct {
	JSON string `json:"json"`
	Name string `json:"name"`
	Skip bool   `json:"skip"`
}

// IndexFile is the response shape for GET /items/{subject}/index.json.
type IndexFile struct {
	Type  string      `json:"type"`  // always "index" for directory listings
	Title string      `json:"title"`
	List  []IndexItem `json:"list"`
}
