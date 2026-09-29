package ports

// FileStatus is the change type of one file in a push.
type FileStatus string

const (
	FileAdded    FileStatus = "added"
	FileModified FileStatus = "modified"
	FileRemoved  FileStatus = "removed"
	FileRenamed  FileStatus = "renamed"
)

// ChangedFile is one entry of a push's changed-file list as reported by the git host.
type ChangedFile struct {
	Path         string     `json:"path"`
	PreviousPath string     `json:"previous_path,omitempty"`
	Status       FileStatus `json:"status"`
	// Similarity is the git host's rename-similarity index (0-100); meaningful for renames only.
	Similarity int `json:"similarity,omitempty"`
}
