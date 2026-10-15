package contract

// WaitInput blocks until a comment on the task carries a tag matching the
// glob Tag. AfterID is the event id to wait after; zero means now. Timeout
// is a Go duration such as `10m`; empty means 10 minutes.
type WaitInput struct {
	ProjectSelector
	TaskID  int64  `json:"task_id"`
	Tag     string `json:"tag"`
	Timeout string `json:"timeout,omitempty"`
	AfterID int64  `json:"after_id,omitempty"`
}

// WaitResponse carries the matching comments, or TimedOut. AfterID is the
// cursor to pass to the next wait.
type WaitResponse struct {
	Project  ProjectSummary   `json:"project"`
	TaskID   int64            `json:"task_id"`
	Matched  []CommentSummary `json:"matched"`
	TimedOut bool             `json:"timed_out"`
	AfterID  int64            `json:"after_id"`
}
