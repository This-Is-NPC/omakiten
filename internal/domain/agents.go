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
	TeamSpawned    = "spawned"
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

// Agent session statuses.
const (
	SessionSpawned  = "spawned"
	SessionFailed   = "failed"
	SessionDone     = "done"
	SessionStopped  = "stopped"
	SessionReplaced = "replaced"
)

// AgentRole is one team seat: the command it runs and the optional harness,
// model, launcher, and brief that override the adapter defaults.
type AgentRole struct {
	Key      string `json:"key"`
	Command  string `json:"command"`
	Harness  string `json:"harness,omitempty"`
	Model    string `json:"model,omitempty"`
	Launcher string `json:"launcher,omitempty"`
	Brief    string `json:"brief,omitempty"`
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

// HarnessSpec is the command line that starts an agent harness.
type HarnessSpec struct {
	Argv []AgentArg `json:"argv"`
}

// LauncherSpec opens a terminal pane and types text into it. Pane is the dot
// path of the pane id in Open's JSON output; empty means the whole output.
// Run, when set, starts the command in the opened pane. Send types the text;
// Submit, when set, presses Enter in a separate command, so a TUI does not
// read text and Enter as one paste.
type LauncherSpec struct {
	Target string     `json:"target,omitempty"`
	Open   []AgentArg `json:"open"`
	Pane   string     `json:"pane,omitempty"`
	Run    []AgentArg `json:"run,omitempty"`
	Send   []AgentArg `json:"send"`
	Submit []AgentArg `json:"submit,omitempty"`
}

// AgentDefaults names the harness, model, and launcher a role without its
// own falls back to.
type AgentDefaults struct {
	Harness  string `json:"harness,omitempty"`
	Model    string `json:"model,omitempty"`
	Launcher string `json:"launcher,omitempty"`
}

// AgentAdapters is the effective agent configuration of a project. Fallback
// holds the user's preferred harness and launcher, used when the selected
// one is not installed. Zero limits mean unlimited.
type AgentAdapters struct {
	Defaults    AgentDefaults           `json:"defaults"`
	Fallback    AgentDefaults           `json:"fallback"`
	Harnesses   map[string]HarnessSpec  `json:"harnesses,omitempty"`
	Launchers   map[string]LauncherSpec `json:"launchers,omitempty"`
	MaxSessions int                     `json:"max_sessions,omitempty"`
	MaxMembers  int                     `json:"max_members,omitempty"`
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

// AgentSession is one agent process opened in a terminal pane.
type AgentSession struct {
	ID         string `json:"session_id"`
	TeamID     string `json:"team_id"`
	Member     string `json:"member"`
	Command    string `json:"command"`
	Persona    string `json:"persona,omitempty"`
	Harness    string `json:"harness"`
	Model      string `json:"model,omitempty"`
	Launcher   string `json:"launcher"`
	Pane       string `json:"pane,omitempty"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	CreatedAt  string `json:"created_at"`
	FinishedAt string `json:"finished_at,omitempty"`
}

// AgentFallback reports a harness or launcher replaced by the user's
// preference because the selected one is not installed.
type AgentFallback struct {
	Harness  string `json:"harness,omitempty"`
	Launcher string `json:"launcher,omitempty"`
	Reason   string `json:"reason"`
}

// MemberOutcome is the result of opening one team seat.
type MemberOutcome struct {
	Key       string         `json:"key"`
	SessionID string         `json:"session_id,omitempty"`
	Status    string         `json:"status"`
	Command   string         `json:"command"`
	Persona   string         `json:"persona,omitempty"`
	Harness   string         `json:"harness,omitempty"`
	Model     string         `json:"model,omitempty"`
	Launcher  string         `json:"launcher,omitempty"`
	Pane      string         `json:"pane,omitempty"`
	Fallback  *AgentFallback `json:"fallback,omitempty"`
	Error     *AgentError    `json:"error,omitempty"`
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
	EventTypeAgentSessionStarted = "agent.session_started"
	EventTypeAgentSessionFailed  = "agent.session_failed"
	EventTypeTeamMemberDone      = "team.member_done"
	EventTypeTeamClosed          = "team.closed"
)
