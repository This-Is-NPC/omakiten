package contract

// CLIInventory is a generated command tree consumed by project knowledge.
type CLIInventory struct {
	Version  int          `json:"version" yaml:"version"`
	Commands []CLICommand `json:"commands" yaml:"commands"`
}

// CLICommand is one command from the executable's command tree.
type CLICommand struct {
	ID          string    `json:"id" yaml:"id"`
	Parent      string    `json:"parent,omitempty" yaml:"parent"`
	Name        string    `json:"name" yaml:"name"`
	Summary     string    `json:"summary,omitempty" yaml:"summary"`
	Description string    `json:"description,omitempty" yaml:"description"`
	Flags       []CLIFlag `json:"flags,omitempty" yaml:"flags"`
	Links       []string  `json:"links,omitempty" yaml:"links"`
}

// CLIFlag describes an option accepted by one command.
type CLIFlag struct {
	Name        string `json:"name" yaml:"name"`
	Shorthand   string `json:"shorthand,omitempty" yaml:"shorthand"`
	Description string `json:"description,omitempty" yaml:"description"`
}
