package obsidianoid

// TreeNode is the recursive JSON structure returned by GET /api/tree.
// JSON tags must match exactly what the frontend expects.
type TreeNode struct {
	Name  string `json:"name"`
	Path  string `json:"path,omitempty"`
	IsDir bool   `json:"is_dir"`
	// Mtime is the last-modified time in Unix milliseconds: a note's own, or
	// for a folder the newest note beneath it, so "most recent" sorts folders
	// and notes alike.
	Mtime    int64       `json:"mtime,omitempty"`
	Children []*TreeNode `json:"children,omitempty"`
}

// Thread is one fixed-slot thread card.
type Thread struct {
	Content  string `json:"content"`
	Disabled bool   `json:"disabled"`
	// Title is the thread's display name, kept in app state rather than in
	// the vault file; empty means the default "THREAD_NN" label.
	Title string `json:"title"`
}

// vaultInfo is the public vault shape — filesystem path intentionally omitted.
type vaultInfo struct {
	Name  string `json:"name"`
	Theme string `json:"theme"`
}

// gitSyncResult is returned by POST /api/git/sync.
type gitSyncResult struct {
	OK     bool   `json:"ok"`
	Output string `json:"output"`
}
