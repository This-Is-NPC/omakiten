package config

type Bundle struct {
	Version   int        `yaml:"version" json:"version"`
	Kit       Kit        `yaml:"kit" json:"kit"`
	Config    Settings   `yaml:"config" json:"config"`
	Skills    []Skill    `yaml:"skills" json:"skills,omitempty"`
	Personas  []Persona  `yaml:"personas" json:"personas,omitempty"`
	Laws      []Law      `yaml:"laws" json:"laws,omitempty"`
	Workflows []Workflow `yaml:"workflows" json:"workflows,omitempty"`
}

type Kit struct {
	ID   int    `yaml:"id" json:"id"`
	Key  string `yaml:"key" json:"key"`
	Name string `yaml:"name" json:"name"`
}

type Settings struct {
	Output   OutputSettings   `yaml:"output" json:"output"`
	Context  ContextSettings  `yaml:"context" json:"context"`
	Workflow WorkflowSettings `yaml:"workflow" json:"workflow"`
	Theme    ThemeSettings    `yaml:"theme" json:"theme"`
}

type OutputSettings struct {
	JSONMinified bool `yaml:"json_minified" json:"json_minified"`
	OmitEmpty    bool `yaml:"omit_empty" json:"omit_empty"`
}

type ContextSettings struct {
	DefaultLevel int `yaml:"default_level" json:"default_level"`
	MaxTokens    int `yaml:"max_tokens" json:"max_tokens"`
}

type WorkflowSettings struct {
	Active string `yaml:"active" json:"active"`
}

type ThemeSettings struct {
	Active string `yaml:"active" json:"active"`
}

type Skill struct {
	ID   int    `yaml:"id" json:"id"`
	Key  string `yaml:"key" json:"key"`
	Name string `yaml:"name" json:"name"`
}

type Persona struct {
	ID       int    `yaml:"id" json:"id"`
	Key      string `yaml:"key" json:"key"`
	Name     string `yaml:"name" json:"name"`
	SkillIDs []int  `yaml:"skill_ids" json:"skill_ids,omitempty"`
}

type Law struct {
	ID       int    `yaml:"id" json:"id"`
	Key      string `yaml:"key" json:"key"`
	Severity string `yaml:"severity" json:"severity"`
	Body     string `yaml:"body" json:"body"`
}

type Workflow struct {
	ID          int          `yaml:"id" json:"id"`
	Key         string       `yaml:"key" json:"key"`
	Name        string       `yaml:"name" json:"name"`
	Buckets     []Bucket     `yaml:"buckets" json:"buckets"`
	Transitions []Transition `yaml:"transitions" json:"transitions,omitempty"`
}

type Bucket struct {
	ID       int    `yaml:"id" json:"id"`
	Key      string `yaml:"key" json:"key"`
	Name     string `yaml:"name" json:"name"`
	Position int    `yaml:"position" json:"position"`
}

type Transition struct {
	From int `yaml:"from" json:"from"`
	To   int `yaml:"to" json:"to"`
}

type Theme struct {
	Version int               `yaml:"version" json:"version"`
	Key     string            `yaml:"key" json:"key"`
	Name    string            `yaml:"name" json:"name"`
	Colors  map[string]string `yaml:"colors" json:"colors"`
}
