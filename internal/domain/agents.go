package domain

// Agent team triggers, the lead's reserved member key, and the comment tag
// prefixes of the team convention.
const (
	AgentTriggerAuto   = "auto"
	AgentTriggerManual = "manual"
	AgentLeadKey       = "lead"
	AgentResultTag     = "review-"
	AgentAddressTag    = "to-"
)

// Team outcome statuses.
const (
	TeamStarted    = "started"
	TeamFailed     = "failed"
	TeamRunning    = "running"
	TeamSuggested  = "suggested"
	TeamSkipped    = "skipped"
	TeamWouldSpawn = "would_spawn"
	TeamClosed     = "closed"
	// TeamUnconfigured marks a team, or a seat, that names no harness or
	// launcher anywhere: role, preset, or preferences.
	TeamUnconfigured = "unconfigured"
)

// TeamStatusActive is the stored status of a team whose seats may run;
// failed and closed teams carry TeamFailed and TeamClosed.
const TeamStatusActive = "active"

// Agent session statuses. A running session's supervisor process is alive;
// the others are final.
const (
	SessionRunning  = "running"
	SessionFailed   = "failed"
	SessionDone     = "done"
	SessionStopped  = "stopped"
	SessionReplaced = "replaced"
)

// AgentRole is one team seat: the command it runs and the optional harness,
// model, and timeout that override the adapter defaults, and its brief.
// Timeout is a Go duration; empty lets the run last until it exits.
type AgentRole struct {
	Key     string `json:"key"`
	Command string `json:"command"`
	Harness string `json:"harness,omitempty"`
	Model   string `json:"model,omitempty"`
	Timeout string `json:"timeout,omitempty"`
	Brief   string `json:"brief,omitempty"`
}

// BucketAgents is the team a bucket opens for a task that enters it.
type BucketAgents struct {
	Trigger string      `json:"trigger"`
	Lead    AgentRole   `json:"lead"`
	Members []AgentRole `json:"members,omitempty"`
}

// AgentArg is one argv element of an adapter template. A group expands to
// all of its values, or to nothing when any of them expands empty.
type AgentArg struct {
	Values []string `json:"values"`
	Group  bool     `json:"group,omitempty"`
}

// HarnessSpec is the command line that runs an agent harness headless: it
// does the work and exits.
type HarnessSpec struct {
	Argv []AgentArg `json:"argv"`
}

// AgentDefaults names the harness, model, and timeout a role without its
// own falls back to.
type AgentDefaults struct {
	Harness string `json:"harness,omitempty"`
	Model   string `json:"model,omitempty"`
	Timeout string `json:"timeout,omitempty"`
}

// AgentAdapters is the effective agent configuration of a project. Fallback
// holds the user's preferred harness, used when the selected one is not
// installed. Zero limits mean unlimited.
type AgentAdapters struct {
	Defaults    AgentDefaults          `json:"defaults"`
	Fallback    AgentDefaults          `json:"fallback"`
	Harnesses   map[string]HarnessSpec `json:"harnesses,omitempty"`
	MaxSessions int                    `json:"max_sessions,omitempty"`
	MaxMembers  int                    `json:"max_members,omitempty"`
}

// AgentTeam is the set of sessions opened for one task in one bucket.
type AgentTeam struct {
	ID        string `json:"team_id"`
	ProjectID int64  `json:"project_id"`
	TaskID    int64  `json:"task_id"`
	BucketKey string `json:"bucket"`
	Status    string `json:"status"`
	// OpenedAfter is the newest event id when the team opened.
	OpenedAfter int64          `json:"opened_after_event_id"`
	CreatedAt   string         `json:"created_at"`
	Sessions    []AgentSession `json:"sessions,omitempty"`
}

// AgentTeamFilter scopes a team listing; zero fields do not filter.
type AgentTeamFilter struct {
	TaskID int64
	Status string
}

// AgentSession is one headless run of a harness under its supervisor
// process. PID leads the harness process group, set once the harness runs;
// Transcript is the JSONL file holding the harness output; ExitCode is set
// once the harness exited.
type AgentSession struct {
	ID         string `json:"session_id"`
	TeamID     string `json:"team_id"`
	Member     string `json:"member"`
	Command    string `json:"command"`
	Persona    string `json:"persona,omitempty"`
	Harness    string `json:"harness"`
	Model      string `json:"model,omitempty"`
	PID        int    `json:"pid,omitempty"`
	Transcript string `json:"transcript,omitempty"`
	Status     string `json:"status"`
	ExitCode   *int   `json:"exit_code,omitempty"`
	Error      string `json:"error,omitempty"`
	CreatedAt  string `json:"created_at"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// TeamSession is a session with the task of its team.
type TeamSession struct {
	Session AgentSession
	TaskID  int64
}

// AgentFallback reports a harness replaced by the user's preference because
// the selected one is not installed.
type AgentFallback struct {
	Harness string `json:"harness"`
	Reason  string `json:"reason"`
}

// MemberOutcome is the result of starting one team seat.
type MemberOutcome struct {
	Key        string         `json:"key"`
	SessionID  string         `json:"session_id,omitempty"`
	Status     string         `json:"status"`
	Command    string         `json:"command"`
	Persona    string         `json:"persona,omitempty"`
	Harness    string         `json:"harness,omitempty"`
	Model      string         `json:"model,omitempty"`
	Transcript string         `json:"transcript,omitempty"`
	Fallback   *AgentFallback `json:"fallback,omitempty"`
	Error      *AgentError    `json:"error,omitempty"`
}

// AgentError is a structured spawn failure. PreferencesPath is the user's
// preferences file, set when a seat is agent_unconfigured.
type AgentError struct {
	Code            string `json:"code"`
	Message         string `json:"message"`
	PreferencesPath string `json:"preferences_path,omitempty"`
}

// TeamOutcome reports what a move or spawn did about a bucket's team.
type TeamOutcome struct {
	Status  string          `json:"status"`
	TeamID  string          `json:"team_id,omitempty"`
	TaskID  int64           `json:"task_id"`
	Bucket  string          `json:"bucket"`
	Lead    MemberOutcome   `json:"lead"`
	Members []MemberOutcome `json:"members,omitempty"`
	Spawn   string          `json:"spawn,omitempty"`
	Follow  []string        `json:"follow,omitempty"`
	Error   *AgentError     `json:"error,omitempty"`
}

// Event types recorded for agent sessions and teams.
const (
	EventTypeAgentSessionStarted  = "agent.session_started"
	EventTypeAgentSessionFinished = "agent.session_finished"
	EventTypeTeamClosed           = "team.closed"
)
