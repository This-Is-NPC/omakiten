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

// AgentLauncher resolves roles to adapters and runs them in panes.
type AgentLauncher interface {
	Resolve(adapters domain.AgentAdapters, role domain.AgentRole) (agentlaunch.Plan, *domain.AgentFallback, *domain.AgentError)
	Open(ctx context.Context, plan agentlaunch.Plan, req agentlaunch.Request) (string, error)
	Send(ctx context.Context, spec domain.LauncherSpec, pane, text string) error
}

// taskService builds the task service whose moves open or plan the agent
// team of the destination bucket.
func (s *Service) taskService() *app.TaskService {
	return app.NewTaskServiceFromStore(s.repo, s.registry, s.snapshot).WithTeamSpawner(teamSpawner{s})
}

// teamSpawner is the app.TeamSpawner backed by this service.
type teamSpawner struct{ s *Service }

func (t teamSpawner) SpawnTeam(ctx context.Context, project domain.ProjectContext, task domain.Task, team domain.BucketAgents) domain.TeamOutcome {
	return t.s.spawnTeam(ctx, project, task, team)
}

func (t teamSpawner) PlanTeam(task domain.Task, team domain.BucketAgents, status string) domain.TeamOutcome {
	out, _ := t.s.planTeam(task, team, status)
	return out
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
	detail, err := s.loadTaskDetail(ctx, input.ProjectSelector, input.TaskID)
	if err != nil {
		return contract.SpawnAgentsResponse{}, err
	}
	team, err := s.requestedTeam(detail.task, input)
	if err != nil {
		return contract.SpawnAgentsResponse{}, err
	}
	outcome := s.spawnTeam(ctx, detail.project, detail.task, team)
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
	if perr != nil {
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
	fail := func(code, message string) domain.TeamOutcome {
		out.Status, out.Error = domain.TeamFailed, &domain.AgentError{Code: code, Message: message}
		return out
	}
	if s.spawnBlocked != "" {
		return fail("store_not_inheritable", s.spawnBlocked)
	}
	if out.Lead.Error != nil {
		return fail(out.Lead.Error.Code, out.Lead.Error.Message)
	}
	adapters := s.snapshot.For(task).AgentAdapters()
	if adapters.MaxMembers > 0 && len(team.Members) > adapters.MaxMembers {
		return fail("max_members", fmt.Sprintf("the team has %d members; preferences allow %d", len(team.Members), adapters.MaxMembers))
	}
	if adapters.MaxSessions > 0 {
		running, err := s.repo.CountSpawnedAgentSessions(ctx, project.ID)
		if err != nil {
			return fail("store_error", err.Error())
		}
		if running+1+len(team.Members) > adapters.MaxSessions {
			return fail("max_sessions", fmt.Sprintf("%d sessions are running; preferences allow %d", running, adapters.MaxSessions))
		}
	}
	opened, created, err := s.repo.OpenAgentTeam(ctx, domain.AgentTeam{ID: newAgentID("t"), ProjectID: project.ID, TaskID: task.ID, BucketKey: task.BucketKey})
	if err != nil {
		return fail("store_error", err.Error())
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
