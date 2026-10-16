package operation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"omakiten/internal/agentlaunch"
	"omakiten/internal/app"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/paths"
)

// agentUnconfigured is the AgentError code of a seat that names no harness
// or launcher anywhere.
const agentUnconfigured = string(domain.ErrAgentUnconfigured)

// AgentLauncher resolves roles to adapters and runs them in panes.
type AgentLauncher interface {
	Resolve(adapters domain.AgentAdapters, role domain.AgentRole) (agentlaunch.Plan, *domain.AgentFallback, *domain.AgentError)
	Open(ctx context.Context, plan agentlaunch.Plan, req agentlaunch.Request) (string, error)
	Send(ctx context.Context, spec domain.LauncherSpec, pane, text string) error
}

// taskService builds the task service whose moves open or plan the agent
// team of the destination bucket. noSpawn reports an automatic team as
// skipped instead of opening it.
func (s *Service) taskService(noSpawn bool) *app.TaskService {
	return app.NewTaskServiceFromStore(s.repo, s.registry, s.snapshot).WithAgentTeams(teamSpawner{s: s, noSpawn: noSpawn})
}

// teamSpawner is the app.AgentTeams backed by this service.
type teamSpawner struct {
	s       *Service
	noSpawn bool
}

func (t teamSpawner) SpawnTeam(ctx context.Context, project domain.ProjectContext, task domain.Task, team domain.BucketAgents) domain.TeamOutcome {
	if t.noSpawn {
		return t.PlanTeam(task, team, domain.TeamSkipped)
	}
	return t.s.spawnTeam(ctx, project, task, team)
}

func (t teamSpawner) PlanTeam(task domain.Task, team domain.BucketAgents, status string) domain.TeamOutcome {
	out, _ := t.s.planTeam(task, team, status)
	return out
}

func (t teamSpawner) LeaveBucket(ctx context.Context, project domain.ProjectContext, task domain.Task, bucket string) {
	ids, err := t.s.repo.ActiveAgentTeamIDs(ctx, project.ID, task.ID)
	if err != nil {
		return
	}
	for _, id := range ids {
		team, err := t.s.repo.GetAgentTeam(ctx, id)
		if err == nil && team.BucketKey == bucket {
			_, _ = t.s.closeTeam(ctx, project, team, fmt.Sprintf("okt: task #%d left %s; stop working on it", task.ID, bucket), "left_bucket")
		}
	}
}

// closeTeam closes an active team, tells each seat it stopped with line, and
// records team.closed with reason.
func (s *Service) closeTeam(ctx context.Context, project domain.ProjectContext, team domain.AgentTeam, line, reason string) (domain.AgentTeam, error) {
	closed, err := s.repo.CloseAgentTeam(ctx, team.ID)
	if err != nil {
		return domain.AgentTeam{}, err
	}
	if !closed {
		return domain.AgentTeam{}, domain.NewError(domain.ErrValidation, "the agent team is not active", map[string]any{"team_id": team.ID, "status": team.Status})
	}
	for _, session := range team.Sessions {
		_ = s.wake(ctx, session, line)
	}
	if body, err := json.Marshal(map[string]any{"team_id": team.ID, "bucket": team.BucketKey, "reason": reason}); err == nil {
		_ = s.repo.RecordEntityEvent(ctx, domain.EventEntityTask, team.TaskID, project.ID, domain.EventTypeTeamClosed, string(body))
	}
	return s.repo.GetAgentTeam(ctx, team.ID)
}

func (s *Service) CloseTeam(ctx context.Context, input contract.CloseTeamInput) (contract.CloseTeamResponse, error) {
	if err := s.allow("team.close"); err != nil {
		return contract.CloseTeamResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.CloseTeamResponse{}, err
	}
	team, err := s.projectTeam(ctx, project, strings.TrimSpace(input.TeamID))
	if err != nil {
		return contract.CloseTeamResponse{}, err
	}
	closed, err := s.closeTeam(ctx, project, team, fmt.Sprintf("okt: team %s was closed; stop working on task #%d", team.ID, team.TaskID), "closed")
	if err != nil {
		return contract.CloseTeamResponse{}, err
	}
	return contract.CloseTeamResponse{Project: projectSummary(project), Team: closed}, nil
}

// SetAgentLauncher replaces the process launcher.
func (s *Service) SetAgentLauncher(l AgentLauncher) {
	s.launcher = l
}

// BlockAgentSpawn makes every spawn fail with reason. Delivery adapters call
// it when the store was selected in a way a child agent cannot inherit.
func (s *Service) BlockAgentSpawn(reason string) {
	s.spawnBlocked = reason
}

func (s *Service) SpawnAgents(ctx context.Context, input contract.SpawnAgentsInput) (contract.SpawnAgentsResponse, error) {
	if err := s.allow("agent.spawn"); err != nil {
		return contract.SpawnAgentsResponse{}, err
	}
	if strings.TrimSpace(input.TeamID) != "" {
		return s.respawnMember(ctx, input)
	}
	detail, err := s.loadTaskDetail(ctx, input.ProjectSelector, input.TaskID)
	if err != nil {
		return contract.SpawnAgentsResponse{}, err
	}
	team, err := s.requestedTeam(detail.task, input)
	if err != nil {
		return contract.SpawnAgentsResponse{}, err
	}
	outcome := s.spawnTeam(ctx, detail.project, detail.task, team)
	if outcome.Status == domain.TeamUnconfigured {
		return contract.SpawnAgentsResponse{}, domain.NewError(domain.ErrAgentUnconfigured, outcome.Error.Message, map[string]any{"team": outcome})
	}
	if outcome.Status == domain.TeamFailed {
		return contract.SpawnAgentsResponse{}, domain.NewError(domain.ErrAgentSpawnFailed, "agents could not be opened", map[string]any{"team": outcome})
	}
	return contract.SpawnAgentsResponse{Project: projectSummary(detail.project), Team: outcome}, nil
}

// requestedTeam is the bucket's declared team, or a team of one lead running
// input.Command; the input's adapters override the lead's.
func (s *Service) requestedTeam(task domain.Task, input contract.SpawnAgentsInput) (domain.BucketAgents, error) {
	var team domain.BucketAgents
	if command := strings.TrimSpace(input.Command); command != "" {
		team = domain.BucketAgents{Trigger: domain.AgentTriggerManual, Lead: domain.AgentRole{Key: domain.AgentLeadKey, Command: command}}
	} else {
		bucket, _ := s.snapshot.For(task).BucketByKey(task.BucketKey)
		if bucket.Agents == nil {
			return team, domain.NewError(domain.ErrValidation, "the task's bucket declares no agents; pass --command", map[string]any{"bucket": task.BucketKey})
		}
		team = *bucket.Agents
	}
	team.Lead.Harness = first(input.Harness, team.Lead.Harness)
	team.Lead.Model = first(input.Model, team.Lead.Model)
	team.Lead.Launcher = first(input.Launcher, team.Lead.Launcher)
	return team, nil
}

// respawnMember reopens one roster seat of an active team in a new session;
// the seat's previous session becomes replaced.
func (s *Service) respawnMember(ctx context.Context, input contract.SpawnAgentsInput) (contract.SpawnAgentsResponse, error) {
	target, err := s.respawnTarget(ctx, input)
	if err != nil {
		return contract.SpawnAgentsResponse{}, err
	}
	team, role := target.team, target.role
	lead := teamSeat(team, domain.AgentLeadKey).ID
	seat, plan := s.planSeat(s.snapshot.For(target.task).AgentAdapters(), role, domain.TeamSpawned)
	if seat.Error == nil {
		if old := teamSeat(team, role.Key); old.ID != "" {
			if err := s.repo.ReplaceAgentSession(ctx, old.ID); err != nil {
				return contract.SpawnAgentsResponse{}, err
			}
		}
		seat = s.openSeat(ctx, target.project, target.task, team.ID, target.roster, role, plan, seat, lead)
	}
	out := domain.TeamOutcome{Status: domain.TeamSpawned, TeamID: team.ID, TaskID: team.TaskID, Bucket: team.BucketKey, Follow: teamFollow(team.ID, team.TaskID, lead)}
	if role.Key == domain.AgentLeadKey {
		out.Lead = seat
	} else {
		out.Members = []domain.MemberOutcome{seat}
	}
	if seat.Error != nil {
		out.Status, out.Error = domain.TeamFailed, seat.Error
		return contract.SpawnAgentsResponse{}, domain.NewError(domain.ErrAgentSpawnFailed, "the seat could not be reopened", map[string]any{"team": out})
	}
	return contract.SpawnAgentsResponse{Project: projectSummary(target.project), Team: out}, nil
}

// seatTarget is the active team, task, roster, and role a respawn reopens.
type seatTarget struct {
	project domain.ProjectContext
	team    domain.AgentTeam
	task    domain.Task
	roster  domain.BucketAgents
	role    domain.AgentRole
}

// respawnTarget validates a respawn request and resolves the seat it names,
// with the request's adapters applied to the role.
func (s *Service) respawnTarget(ctx context.Context, input contract.SpawnAgentsInput) (seatTarget, error) {
	if strings.TrimSpace(input.Member) == "" || strings.TrimSpace(input.Command) != "" {
		return seatTarget{}, domain.NewError(domain.ErrValidation, "--team reopens one roster seat: pass --member and no --command", nil)
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return seatTarget{}, err
	}
	team, err := s.projectTeam(ctx, project, strings.TrimSpace(input.TeamID))
	if err != nil {
		return seatTarget{}, err
	}
	if team.Status != domain.TeamStatusActive {
		return seatTarget{}, domain.NewError(domain.ErrValidation, "the agent team is not active", map[string]any{"team_id": team.ID, "status": team.Status})
	}
	detail, err := s.loadTaskDetail(ctx, input.ProjectSelector, team.TaskID)
	if err != nil {
		return seatTarget{}, err
	}
	task := detail.task
	task.BucketKey = team.BucketKey
	bucket, _ := s.snapshot.For(task).BucketByKey(team.BucketKey)
	roster, role, err := rosterSeat(bucket.Agents, strings.TrimSpace(input.Member))
	if err != nil {
		return seatTarget{}, err
	}
	role.Harness, role.Model, role.Launcher = first(input.Harness, role.Harness), first(input.Model, role.Model), first(input.Launcher, role.Launcher)
	return seatTarget{project: project, team: team, task: task, roster: roster, role: role}, nil
}

// rosterSeat returns the bucket team and the role a member key names in it.
func rosterSeat(team *domain.BucketAgents, key string) (domain.BucketAgents, domain.AgentRole, error) {
	if team == nil {
		return domain.BucketAgents{}, domain.AgentRole{}, domain.NewError(domain.ErrValidation, "the team's bucket declares no agents", nil)
	}
	keys := []string{domain.AgentLeadKey}
	if key == domain.AgentLeadKey {
		return *team, team.Lead, nil
	}
	for _, m := range team.Members {
		if m.Key == key {
			return *team, m, nil
		}
		keys = append(keys, m.Key)
	}
	return domain.BucketAgents{}, domain.AgentRole{}, domain.NewError(domain.ErrValidation, "member is not in the bucket's roster", map[string]any{"member": key, "roster": keys})
}

func first(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// planTeam resolves every seat without opening anything. plans[0] is the
// lead's; the rest follow team.Members.
func (s *Service) planTeam(task domain.Task, team domain.BucketAgents, status string) (domain.TeamOutcome, []agentlaunch.Plan) {
	adapters := s.snapshot.For(task).AgentAdapters()
	out := domain.TeamOutcome{Status: status, TaskID: task.ID, Bucket: task.BucketKey}
	var plan agentlaunch.Plan
	out.Lead, plan = s.planSeat(adapters, team.Lead, status)
	plans := []agentlaunch.Plan{plan}
	for _, member := range team.Members {
		seat, plan := s.planSeat(adapters, member, status)
		out.Members = append(out.Members, seat)
		plans = append(plans, plan)
	}
	return out, plans
}

func (s *Service) planSeat(adapters domain.AgentAdapters, role domain.AgentRole, status string) (domain.MemberOutcome, agentlaunch.Plan) {
	seat := domain.MemberOutcome{Key: role.Key, Status: status, Command: role.Command, Persona: s.commandPersona(role.Command)}
	plan, fallback, perr := s.launcher.Resolve(adapters, role)
	seat.Harness, seat.Model, seat.Launcher, seat.Fallback, seat.Error = plan.Harness, plan.Model, plan.Launcher, fallback, perr
	switch {
	case perr != nil && perr.Code == agentUnconfigured:
		seat.Status = domain.TeamUnconfigured
	case perr != nil:
		seat.Status = domain.SessionFailed
	}
	return seat, plan
}

func (s *Service) commandPersona(command string) string {
	if binding, ok := s.loadCommandCatalog()[command]; ok {
		return binding.Persona
	}
	return ""
}

// spawnTeam opens the lead, then every member in parallel, and records the
// team and its sessions. A lead that cannot open fails the whole team.
func (s *Service) spawnTeam(ctx context.Context, project domain.ProjectContext, task domain.Task, team domain.BucketAgents) domain.TeamOutcome {
	out, plans := s.planTeam(task, team, domain.TeamSpawned)
	fail := func(perr *domain.AgentError) domain.TeamOutcome {
		out.Status, out.Error = domain.TeamFailed, perr
		if perr.Code == agentUnconfigured {
			out.Status = domain.TeamUnconfigured
		}
		return out
	}
	if perr := s.spawnRefusal(ctx, project, task, team, out.Lead); perr != nil {
		return fail(perr)
	}
	opened, created, err := s.repo.OpenAgentTeam(ctx, domain.AgentTeam{ID: newAgentID("t"), ProjectID: project.ID, TaskID: task.ID, BucketKey: task.BucketKey})
	if err != nil {
		return fail(&domain.AgentError{Code: "store_error", Message: err.Error()})
	}
	if !created {
		return runningTeam(opened)
	}
	out.TeamID = opened.ID
	roles := append([]domain.AgentRole{team.Lead}, team.Members...)
	out.Lead = s.openSeat(ctx, project, task, opened.ID, team, roles[0], plans[0], out.Lead, "")
	if out.Lead.Status != domain.SessionSpawned {
		_ = s.repo.FailAgentTeam(ctx, opened.ID)
		out.Status, out.Error = domain.TeamFailed, out.Lead.Error
		return out
	}
	var wg sync.WaitGroup
	for i := range out.Members {
		if out.Members[i].Error != nil {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out.Members[i] = s.openSeat(ctx, project, task, opened.ID, team, roles[i+1], plans[i+1], out.Members[i], out.Lead.SessionID)
		}(i)
	}
	wg.Wait()
	out.Follow = teamFollow(opened.ID, task.ID, out.Lead.SessionID)
	return out
}

// spawnRefusal returns why a team must not open before anything starts: an
// uninheritable store, a lead that cannot resolve, or a preference limit.
func (s *Service) spawnRefusal(ctx context.Context, project domain.ProjectContext, task domain.Task, team domain.BucketAgents, lead domain.MemberOutcome) *domain.AgentError {
	if s.spawnBlocked != "" {
		return &domain.AgentError{Code: "store_not_inheritable", Message: s.spawnBlocked}
	}
	if lead.Error != nil {
		return lead.Error
	}
	adapters := s.snapshot.For(task).AgentAdapters()
	if adapters.MaxMembers > 0 && len(team.Members) > adapters.MaxMembers {
		return &domain.AgentError{Code: "max_members", Message: fmt.Sprintf("the team has %d members; preferences allow %d", len(team.Members), adapters.MaxMembers)}
	}
	if adapters.MaxSessions > 0 {
		running, err := s.repo.CountSpawnedAgentSessions(ctx, project.ID)
		if err != nil {
			return &domain.AgentError{Code: "store_error", Message: err.Error()}
		}
		if running+1+len(team.Members) > adapters.MaxSessions {
			return &domain.AgentError{Code: "max_sessions", Message: fmt.Sprintf("%d sessions are running; preferences allow %d", running, adapters.MaxSessions)}
		}
	}
	return nil
}

// openSeat writes the seat's prompt, opens its pane, and records the session
// with its start or failure event.
func (s *Service) openSeat(ctx context.Context, project domain.ProjectContext, task domain.Task, teamID string, team domain.BucketAgents, role domain.AgentRole, plan agentlaunch.Plan, seat domain.MemberOutcome, leadSession string) domain.MemberOutcome {
	seat.SessionID = newAgentID("s")
	prompt, err := s.seatPrompt(ctx, task, teamID, team, role, seat.SessionID, leadSession)
	var pane string
	if err == nil {
		pane, err = s.launcher.Open(ctx, plan, agentlaunch.Request{
			Cwd:        project.RootPath,
			Label:      fmt.Sprintf("okt-%d-%s", task.ID, role.Key),
			PromptFile: prompt,
			Vars:       map[string]string{"session_id": seat.SessionID, "team_id": teamID, "member": role.Key},
			Env:        agentEnv(seat.SessionID, teamID, role.Key, plan),
		})
	}
	seat.Pane = pane
	session := domain.AgentSession{ID: seat.SessionID, TeamID: teamID, Member: role.Key, Command: role.Command, Persona: seat.Persona, Harness: plan.Harness, Model: plan.Model, Launcher: plan.Launcher, Pane: pane, Status: domain.SessionSpawned}
	eventType := domain.EventTypeAgentSessionStarted
	if err != nil {
		seat.Status, seat.Error = domain.SessionFailed, &domain.AgentError{Code: "open_failed", Message: err.Error()}
		session.Status, session.Error = domain.SessionFailed, err.Error()
		eventType = domain.EventTypeAgentSessionFailed
	}
	if err := s.repo.AddAgentSession(ctx, session); err != nil && seat.Error == nil {
		seat.Status, seat.Error = domain.SessionFailed, &domain.AgentError{Code: "store_error", Message: err.Error()}
	}
	payload := map[string]any{"team_id": teamID, "session_id": seat.SessionID, "member": role.Key, "task_id": task.ID, "harness": plan.Harness, "model": plan.Model, "launcher": plan.Launcher, "pane": pane}
	if seat.Error != nil {
		payload["error"] = seat.Error.Message
	}
	if body, err := json.Marshal(payload); err == nil {
		_ = s.repo.RecordEntityEvent(ctx, domain.EventEntityTask, task.ID, project.ID, eventType, string(body))
	}
	return seat
}

// agentEnv is the environment a seat's harness inherits: the store
// selection, the session identity, and PATH led by this executable's
// directory so the agent runs the same okt.
func agentEnv(sessionID, teamID, member string, plan agentlaunch.Plan) []string {
	env := []string{
		"OMAKITEN_AGENT_SESSION_ID=" + sessionID,
		"OMAKITEN_AGENT_MODEL=" + first(plan.Model, plan.Harness),
		"OMAKITEN_TEAM_ID=" + teamID,
		"OMAKITEN_TEAM_MEMBER=" + member,
	}
	if home := os.Getenv("OMAKITEN_HOME"); home != "" {
		env = append(env, "OMAKITEN_HOME="+home)
	}
	if exe, err := os.Executable(); err == nil {
		env = append(env, "PATH="+filepath.Dir(exe)+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	return env
}

// seatPrompt writes the seat's prompt file: the resolved command, the task,
// and the team protocol.
func (s *Service) seatPrompt(ctx context.Context, task domain.Task, teamID string, team domain.BucketAgents, role domain.AgentRole, sessionID, leadSession string) (string, error) {
	command, err := s.ResolveCommand(ctx, contract.ResolveCommandInput{Name: role.Command})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(command.Markdown)
	fmt.Fprintf(&b, "\n\n## Omakiten task\n\nYou work on task #%d %q in bucket %q. Read it with `okt task continue %d`.\n", task.ID, task.Title, task.BucketKey, task.ID)
	fmt.Fprintf(&b, "\n## Your team seat\n\n- Team: `%s`. Your key: `%s`. Your session: `%s`.\n", teamID, role.Key, sessionID)
	if role.Brief != "" {
		fmt.Fprintf(&b, "- Brief: %s\n", role.Brief)
	}
	if role.Key == domain.AgentLeadKey {
		var keys []string
		for _, m := range team.Members {
			keys = append(keys, "`"+m.Key+"`")
		}
		if len(keys) > 0 {
			fmt.Fprintf(&b, "- You lead members %s. Wait for each `%s<key>` result, then decide: `okt wait --task %d --tag '%s*'`.\n", strings.Join(keys, ", "), domain.AgentResultTag, task.ID, domain.AgentResultTag)
		}
	} else {
		fmt.Fprintf(&b, "- Your lead's session is `%s`. Publish your result exactly once: `okt comment add %d -a agent -T %s%s -b \"…\"`.\n", leadSession, task.ID, domain.AgentResultTag, role.Key)
	}
	fmt.Fprintf(&b, "- Ask a teammate: `okt comment add %d -a agent -T %s<key> -b \"…\"`. Read messages for you: `okt comment list %d --tag %s%s`.\n", task.ID, domain.AgentAddressTag, task.ID, domain.AgentAddressTag, role.Key)
	fmt.Fprintf(&b, "- See the team: `okt team show %s`.\n", teamID)
	dir, err := paths.StateDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "agents")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, sessionID+".md")
	return path, os.WriteFile(path, []byte(b.String()), 0o600)
}

func runningTeam(team domain.AgentTeam) domain.TeamOutcome {
	out := domain.TeamOutcome{Status: domain.TeamRunning, TeamID: team.ID, TaskID: team.TaskID, Bucket: team.BucketKey}
	for _, session := range team.Sessions {
		if session.Status == domain.SessionReplaced {
			continue
		}
		seat := domain.MemberOutcome{Key: session.Member, SessionID: session.ID, Status: session.Status, Command: session.Command, Persona: session.Persona, Harness: session.Harness, Model: session.Model, Launcher: session.Launcher, Pane: session.Pane}
		if session.Member == domain.AgentLeadKey {
			out.Lead = seat
			continue
		}
		out.Members = append(out.Members, seat)
	}
	out.Follow = teamFollow(team.ID, team.TaskID, out.Lead.SessionID)
	return out
}

func teamFollow(teamID string, taskID int64, leadSession string) []string {
	id := strconv.FormatInt(taskID, 10)
	return []string{"okt team show " + teamID, "okt comment list " + id + " --tag " + domain.AgentAddressTag + domain.AgentLeadKey, "okt logs --session " + leadSession}
}

func newAgentID(prefix string) string {
	var raw [6]byte
	_, _ = rand.Read(raw[:])
	return prefix + "-" + hex.EncodeToString(raw[:])
}

func (s *Service) ShowAgent(ctx context.Context, input contract.ShowAgentInput) (contract.ShowAgentResponse, error) {
	if err := s.allow("agent.show"); err != nil {
		return contract.ShowAgentResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.ShowAgentResponse{}, err
	}
	session, err := s.repo.GetAgentSession(ctx, strings.TrimSpace(input.SessionID))
	if err != nil {
		return contract.ShowAgentResponse{}, err
	}
	team, err := s.projectTeam(ctx, project, session.TeamID)
	if err != nil {
		return contract.ShowAgentResponse{}, err
	}
	resp := contract.ShowAgentResponse{Project: projectSummary(project), Session: session, TaskID: team.TaskID, Bucket: team.BucketKey,
		Follow: []string{"okt task continue " + strconv.FormatInt(team.TaskID, 10), "okt logs --session " + session.ID, "okt team show " + team.ID}}
	last, err := s.repo.LastAgentActivity(ctx, session.ID)
	if err != nil {
		return contract.ShowAgentResponse{}, err
	}
	if last.ID > 0 {
		resp.LastActivity = &contract.AgentActivity{EventID: last.ID, EventType: last.EventType, CreatedAt: last.CreatedAt}
	}
	return resp, nil
}

func (s *Service) ShowTeam(ctx context.Context, input contract.ShowTeamInput) (contract.ShowTeamResponse, error) {
	if err := s.allow("team.show"); err != nil {
		return contract.ShowTeamResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.ShowTeamResponse{}, err
	}
	team, err := s.projectTeam(ctx, project, strings.TrimSpace(input.TeamID))
	if err != nil {
		return contract.ShowTeamResponse{}, err
	}
	comments, err := s.newCommentService().List(ctx, project, team.TaskID)
	if err != nil {
		return contract.ShowTeamResponse{}, err
	}
	results := []contract.TeamResult{}
	for _, session := range team.Sessions {
		if session.Status == domain.SessionReplaced {
			continue
		}
		for _, c := range comments {
			if hasTag(c.Tags, domain.AgentResultTag+session.Member) {
				results = append(results, contract.TeamResult{Member: session.Member, CommentID: c.ID, CreatedAt: c.CreatedAt})
			}
		}
	}
	id := strconv.FormatInt(team.TaskID, 10)
	return contract.ShowTeamResponse{Project: projectSummary(project), Team: team, Results: results,
		Follow: []string{"okt task continue " + id, "okt wait --task " + id + " --tag '" + domain.AgentResultTag + "*'"}}, nil
}

// projectTeam loads a team and hides teams of other projects.
func (s *Service) projectTeam(ctx context.Context, project domain.ProjectContext, teamID string) (domain.AgentTeam, error) {
	team, err := s.repo.GetAgentTeam(ctx, teamID)
	if err != nil {
		return domain.AgentTeam{}, err
	}
	if team.ProjectID != project.ID {
		return domain.AgentTeam{}, domain.NewError(domain.ErrAgentNotFound, "agent team not found", map[string]any{"team_id": teamID})
	}
	return team, nil
}

func hasTag(tags []domain.Tag, name string) bool {
	for _, t := range tags {
		if t.Name == name {
			return true
		}
	}
	return false
}

// noteTeamComment applies the team convention to a task comment: each
// review-<key> tag finishes that member and wakes the lead, and each
// to-<key> tag wakes the addressed seat. Keys without a seat are ignored, and
// failures never fail the comment.
func (s *Service) noteTeamComment(ctx context.Context, project domain.ProjectContext, comment domain.Comment) {
	results := taggedKeys(comment, domain.AgentResultTag)
	messages := taggedKeys(comment, domain.AgentAddressTag)
	if len(results) == 0 && len(messages) == 0 {
		return
	}
	teams, err := s.repo.ActiveAgentTeamIDs(ctx, project.ID, comment.TaskID)
	if err != nil {
		return
	}
	for _, id := range teams {
		team, err := s.repo.GetAgentTeam(ctx, id)
		if err != nil {
			continue
		}
		s.finishMembers(ctx, project, team, results, comment.ID)
		s.deliverMessages(ctx, team, messages)
	}
}

// taggedKeys returns the keys a task comment's tags name after prefix.
func taggedKeys(comment domain.Comment, prefix string) map[string]bool {
	keys := map[string]bool{}
	if comment.Scope != domain.CommentScopeTask || comment.TaskID == 0 {
		return keys
	}
	for _, tag := range comment.Tags {
		if key, ok := strings.CutPrefix(tag.Name, prefix); ok && key != "" {
			keys[key] = true
		}
	}
	return keys
}

func (s *Service) finishMembers(ctx context.Context, project domain.ProjectContext, team domain.AgentTeam, members map[string]bool, commentID int64) {
	lead := teamSeat(team, domain.AgentLeadKey)
	for _, session := range team.Sessions {
		if !members[session.Member] {
			continue
		}
		if finished, err := s.repo.FinishAgentSession(ctx, session.ID); err != nil || !finished {
			continue
		}
		payload := map[string]any{"team_id": team.ID, "member": session.Member, "session_id": session.ID, "comment_id": commentID}
		if session.Member != domain.AgentLeadKey {
			if err := s.wake(ctx, lead, fmt.Sprintf("okt: %s finished task #%d — okt team show %s", session.Member, team.TaskID, team.ID)); err != nil {
				payload["wake_error"] = err.Error()
			}
		}
		if body, err := json.Marshal(payload); err == nil {
			_ = s.repo.RecordEntityEvent(ctx, domain.EventEntityTask, team.TaskID, project.ID, domain.EventTypeTeamMemberDone, string(body))
		}
	}
}

// deliverMessages wakes each addressed seat with the command that reads
// its messages.
func (s *Service) deliverMessages(ctx context.Context, team domain.AgentTeam, keys map[string]bool) {
	for key := range keys {
		line := fmt.Sprintf("okt: message for %s on task #%d — okt comment list %d --tag %s%s", key, team.TaskID, team.TaskID, domain.AgentAddressTag, key)
		_ = s.wake(ctx, teamSeat(team, key), line)
	}
}

// teamSeat returns the current session of a member key, or a zero session.
func teamSeat(team domain.AgentTeam, key string) domain.AgentSession {
	for _, session := range team.Sessions {
		if session.Member == key && session.Status != domain.SessionReplaced {
			return session
		}
	}
	return domain.AgentSession{}
}

// wake types one line into a running session's pane.
func (s *Service) wake(ctx context.Context, session domain.AgentSession, line string) error {
	if session.Status != domain.SessionSpawned || session.Pane == "" {
		return nil
	}
	return s.launcher.Send(ctx, s.snapshot.AgentAdapters().Launchers[session.Launcher], session.Pane, line)
}

func (s *Service) ListAgents(ctx context.Context, input contract.ListAgentsInput) (contract.ListAgentsResponse, error) {
	if err := s.allow("agent.list"); err != nil {
		return contract.ListAgentsResponse{}, err
	}
	status := strings.TrimSpace(input.Status)
	switch status {
	case "", domain.TeamStatusActive, domain.TeamFailed, domain.TeamClosed:
	default:
		return contract.ListAgentsResponse{}, domain.NewError(domain.ErrValidation, "status must be active, failed, or closed", map[string]any{"status": input.Status})
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.ListAgentsResponse{}, err
	}
	teams, err := s.repo.ListAgentTeams(ctx, project.ID, domain.AgentTeamFilter{TaskID: input.TaskID, Status: status})
	if err != nil {
		return contract.ListAgentsResponse{}, err
	}
	if teams == nil {
		teams = []domain.AgentTeam{}
	}
	return contract.ListAgentsResponse{Project: projectSummary(project), Teams: teams}, nil
}

// activeTeam returns the task's newest active team, or nil.
func (s *Service) activeTeam(ctx context.Context, project domain.ProjectContext, taskID int64) (*domain.AgentTeam, error) {
	teams, err := s.repo.ListAgentTeams(ctx, project.ID, domain.AgentTeamFilter{TaskID: taskID, Status: domain.TeamStatusActive})
	if err != nil || len(teams) == 0 {
		return nil, err
	}
	return &teams[0], nil
}
