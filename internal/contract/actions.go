package contract

// ActionRequest names an application operation and its structured inputs.
type ActionRequest struct {
	Operation string
	Arguments map[string]any
	ProjectID int64
	RuntimeID int64
}

// ActionResult carries presentation-neutral outcome data.
type ActionResult struct {
	Confirmation Confirmation
	Message      string
	Migration    bool
	Migrated     int
}
