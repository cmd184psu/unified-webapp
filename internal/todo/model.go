package todo

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
	Title string      `json:"title"`
	List  []IndexItem `json:"list"`
}
