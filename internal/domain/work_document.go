package domain

// WorkDocument is a portable plan or task with OKF metadata and Markdown content.
type WorkDocument struct {
	Type        string         `yaml:"type" json:"type"`
	Title       string         `yaml:"title" json:"title"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Spec        WorkSpec       `yaml:"omakiten" json:"omakiten"`
	Metadata    map[string]any `yaml:",inline" json:"metadata,omitempty"`
	Body        string         `yaml:"-" json:"body"`
}

// WorkSpec carries the versioned Omakiten structure inside an OKF concept.
type WorkSpec struct {
	Version  int            `yaml:"version" json:"version"`
	Slug     string         `yaml:"slug,omitempty" json:"slug,omitempty"`
	Status   string         `yaml:"status,omitempty" json:"status,omitempty"`
	Task     *WorkTask      `yaml:"task,omitempty" json:"task,omitempty"`
	Waves    []WorkWave     `yaml:"waves,omitempty" json:"waves,omitempty"`
	Tasks    []WorkTask     `yaml:"tasks,omitempty" json:"tasks,omitempty"`
	Metadata map[string]any `yaml:",inline" json:"metadata,omitempty"`
}

// WorkWave orders a group of tasks using file-local references.
type WorkWave struct {
	Key      string         `yaml:"key" json:"key"`
	Name     string         `yaml:"name" json:"name"`
	Tasks    []WorkTask     `yaml:"tasks,omitempty" json:"tasks,omitempty"`
	Metadata map[string]any `yaml:",inline" json:"metadata,omitempty"`
}

// WorkTask describes task content and relationships independently of database IDs.
type WorkTask struct {
	Key         string         `yaml:"key" json:"key"`
	Title       string         `yaml:"title,omitempty" json:"title,omitempty"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Priority    string         `yaml:"priority,omitempty" json:"priority,omitempty"`
	Bucket      string         `yaml:"bucket,omitempty" json:"bucket,omitempty"`
	State       string         `yaml:"state,omitempty" json:"state,omitempty"`
	Assignee    string         `yaml:"assignee,omitempty" json:"assignee,omitempty"`
	Parent      string         `yaml:"parent,omitempty" json:"parent,omitempty"`
	PlanMember  *bool          `yaml:"plan_member,omitempty" json:"plan_member,omitempty"`
	DependsOn   []string       `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`
	Tags        []string       `yaml:"tags,omitempty" json:"tags,omitempty"`
	Metadata    map[string]any `yaml:",inline" json:"metadata,omitempty"`
}

// WorkRecord supplies complete persisted content for document export.
type WorkRecord struct {
	Plan         *Plan
	Waves        []PlanWave
	Tasks        []WorkTaskRecord
	Dependencies []TaskDependency
	Metadata     []byte
}

type WorkTaskRecord struct {
	Task     Task
	WaveID   int64
	PlanID   int64
	Assignee string
	Metadata []byte
	Tags     []string
}

// TaskList returns all document tasks in source order.
func (doc WorkDocument) TaskList() []WorkTask {
	tasks := append([]WorkTask(nil), doc.Spec.Tasks...)
	if doc.Spec.Task != nil {
		tasks = append([]WorkTask{*doc.Spec.Task}, tasks...)
	}
	for _, wave := range doc.Spec.Waves {
		tasks = append(tasks, wave.Tasks...)
	}
	return tasks
}
