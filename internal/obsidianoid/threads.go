package obsidianoid

import (
	"fmt"
	"os"
	"path/filepath"
)

func threadFileName(index int) string {
	return fmt.Sprintf("Thread%02d.md", index+1)
}

// ThreadFileName is exported for tests.
func ThreadFileName(index int) string { return threadFileName(index) }

// ReadThreads is exported for tests.
func ReadThreads(vaultPath, folder string, count int, states []ThreadState) ([]Thread, error) {
	return readThreads(vaultPath, folder, count, states)
}

// WriteThreads is exported for tests.
func WriteThreads(vaultPath, folder string, threads []Thread) error {
	return writeThreads(vaultPath, folder, threads)
}

// readThreads reads all thread files from the vault and merges each slot's
// app-side state (disabled flag and title).
func readThreads(vaultPath, folder string, count int, states []ThreadState) ([]Thread, error) {
	result := make([]Thread, count)
	for i := 0; i < count; i++ {
		path := filepath.Join(vaultPath, folder, threadFileName(i))
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("thread %d: %w", i+1, err)
		}
		var st ThreadState
		if i < len(states) {
			st = states[i]
		}
		result[i] = Thread{
			Content:  string(data),
			Disabled: st.Disabled,
			Title:    st.Title,
		}
	}
	return result, nil
}

// writeThreads writes thread content to vault files. Disabled state and titles
// are not stored in files, so the file names never change.
func writeThreads(vaultPath, folder string, threads []Thread) error {
	dir := filepath.Join(vaultPath, folder)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	for i, t := range threads {
		path := filepath.Join(dir, threadFileName(i))
		if err := os.WriteFile(path, []byte(t.Content), 0o600); err != nil {
			return fmt.Errorf("thread %d: %w", i+1, err)
		}
	}
	return nil
}
