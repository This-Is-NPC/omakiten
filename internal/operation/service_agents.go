package operation

import (
	"cmp"
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
	"time"

	"omakiten/internal/agentlaunch"
	"omakiten/internal/app"
	"omakiten/internal/config"
	"omakiten/internal/contract"
	"omakiten/internal/domain"
	"omakiten/internal/paths"
)

// agentUnconfigured is the AgentError code of a seat that names no harness
// anywhere.
const agentUnconfigured = string(domain.ErrAgentUnconfigured)

// orphanGrace is how long a running session may go without its supervisor
// holding the session lock before it counts as orphaned: the time a
// supervisor takes to start and lock.
const orphanGrace = time.Minute

// AgentLauncher resolves roles to harnesses, starts their supervisors, and
// reports and stops running sessions.
type AgentLauncher interface {
	Resolve(adapters domain.AgentAdapters, role domain.AgentRole) (agentlaunch.Plan, *domain.AgentFallback, *domain.AgentError)
	Start(ctx context.Context, plan agentlaunch.Plan, req agentlaunch.Request) error
	Alive(sessionID string) bool
	Stop(pid int) error
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
	out, seats, _ := t.s.planTeam(task, team, status)
	return withSeats(out, seats)
}

func (t teamSpawner) LeaveBucket(ctx context.Context, project domain.ProjectContext, task domain.Task, bucket string) {
	ids, err := t.s.repo.ActiveAgentTeamIDs(ctx, project.ID, task.ID)
	if err != nil {
		return
	}
	for _, id := range ids {
		team, err := t.s.repo.GetAgentTeam(ctx, id)
		if err == nil && team.BucketKey == bucket {
			_, _ = t.s.closeTeam(ctx, project, team, "left_bucket")
		}
	}
}

// closeTeam closes an active team, stops the supervisor of each running
// seat, and records team.closed with reason.
func (s *Service) closeTeam(ctx context.Context, project domain.ProjectContext, team domain.AgentTeam, reason string) (domain.AgentTeam, error) {
	closed, err := s.repo.CloseAgentTeam(ctx, team.ID)
	if err != nil {
		return domain.AgentTeam{}, err
	}
	if !closed {
		return domain.AgentTeam{}, domain.NewError(domain.ErrValidation, "the agent team is not active", map[string]any{"team_id": team.ID, "status": team.Status})
	}
	for _, session := range team.Sessions {
		s.stopSession(session)
	}
	if body, err := json.Marshal(map[string]any{"team_id": team.ID, "task_id": team.TaskID, "bucket": team.BucketKey, "reason": reason}); err == nil {
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
	closed, err := s.closeTeam(ctx, project, team, "closed")
	if err != nil {
		return contract.CloseTeamResponse{}, err
	}
	return contract.CloseTeamResponse{Project: projectSummary(project), Team: closed}, nil
}

// SetAgentLauncher replaces the supervisor launcher.
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
		return contract.SpawnAgentsResponse{}, domain.NewError(domain.ErrAgentUnconfigured, outcome.Error.Message, map[string]any{"team": outcome, "preferences_path": outcome.Error.PreferencesPath})
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
	return team, nil
}

// respawnMember runs one roster seat of an active team again in a new
// session; the seat's previous session is stopped and becomes replaced.
func (s *Service) respawnMember(ctx context.Context, input contract.SpawnAgentsInput) (contract.SpawnAgentsResponse, error) {
	if strings.TrimSpace(input.Member) == "" || strings.TrimSpace(input.Command) != "" {
		return contract.SpawnAgentsResponse{}, domain.NewError(domain.ErrValidation, "--team runs one roster seat again: pass --member and no --command", nil)
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.SpawnAgentsResponse{}, err
	}
	team, err := s.projectTeam(ctx, project, strings.TrimSpace(input.TeamID))
	if err != nil {
		return contract.SpawnAgentsResponse{}, err
	}
	target, err := s.seatTarget(ctx, project, team, strings.TrimSpace(input.Member))
	if err != nil {
		return contract.SpawnAgentsResponse{}, err
	}
	target.role.Harness, target.role.Model = first(input.Harness, target.role.Harness), first(input.Model, target.role.Model)
	return s.startTeamSeat(ctx, target, true)
}

// SpawnSeat starts one roster seat of an active team, named by TeamID or as
// the active team of TaskID, unless the seat already has a session. It is
// what the spawn hook action runs, so a repeated event starts nothing new.
func (s *Service) SpawnSeat(ctx context.Context, input contract.SpawnSeatInput) (contract.SpawnAgentsResponse, error) {
	if err := s.allow("agent.spawn"); err != nil {
		return contract.SpawnAgentsResponse{}, err
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.SpawnAgentsResponse{}, err
	}
	var team domain.AgentTeam
	if id := strings.TrimSpace(input.TeamID); id != "" {
		team, err = s.projectTeam(ctx, project, id)
	} else {
		var active *domain.AgentTeam
		active, err = s.activeTeam(ctx, project, input.TaskID)
		if err == nil && active == nil {
			err = domain.NewError(domain.ErrValidation, "the task has no active agent team", map[string]any{"task_id": input.TaskID})
		}
		if active != nil {
			team = *active
		}
	}
	if err != nil {
		return contract.SpawnAgentsResponse{}, err
	}
	target, err := s.seatTarget(ctx, project, team, strings.TrimSpace(input.Member))
	if err != nil {
		return contract.SpawnAgentsResponse{}, err
	}
	return s.startTeamSeat(ctx, target, false)
}

// startTeamSeat starts the target seat in a new session. With replace, a
// seat that has a session is stopped and replaced; without it, such a seat
// is left as it is and reported.
func (s *Service) startTeamSeat(ctx context.Context, target seatTarget, replace bool) (contract.SpawnAgentsResponse, error) {
	team, role := target.team, target.role
	out := domain.TeamOutcome{Status: domain.TeamStarted, TeamID: team.ID, TaskID: team.TaskID, Bucket: team.BucketKey, Follow: teamFollow(team.ID, team.TaskID)}
	old := teamSeat(team, role.Key)
	if old.ID != "" && !replace {
		out.Status = domain.TeamRunning
		placeSeat(&out, sessionOutcome(old))
		return contract.SpawnAgentsResponse{Project: projectSummary(target.project), Team: out}, nil
	}
	adapters := s.snapshot.For(target.task).AgentAdapters()
	seat, plan := s.planSeat(adapters, role, domain.TeamStarted)
	if seat.Error == nil {
		freed := 0
		if old.Status == domain.SessionRunning {
			freed = 1
		}
		seat.Error = s.spawnRefusal(ctx, target.project, adapters, len(target.roster.Members), 1-freed)
	}
	if seat.Error == nil {
		if old.ID != "" {
			s.stopSession(old)
			if err := s.repo.ReplaceAgentSession(ctx, old.ID); err != nil {
				return contract.SpawnAgentsResponse{}, err
			}
		}
		seat = s.openSeat(ctx, target.project, target.task, team.ID, target.roster, role, plan, seat)
	}
	placeSeat(&out, seat)
	if seat.Error != nil {
		out.Status, out.Error = domain.TeamFailed, seat.Error
		return contract.SpawnAgentsResponse{}, domain.NewError(domain.ErrAgentSpawnFailed, "the seat could not be started", map[string]any{"team": out})
	}
	return contract.SpawnAgentsResponse{Project: projectSummary(target.project), Team: out}, nil
}

// placeSeat puts a seat in a team outcome as its lead or as a member.
func placeSeat(out *domain.TeamOutcome, seat domain.MemberOutcome) {
	if seat.Key == domain.AgentLeadKey {
		out.Lead = seat
		return
	}
	out.Members = append(out.Members, seat)
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

// seatTarget is the active team, task, roster, and role a seat start runs.
type seatTarget struct {
	project domain.ProjectContext
	team    domain.AgentTeam
	task    domain.Task
	roster  domain.BucketAgents
	role    domain.AgentRole
}

// seatTarget resolves the roster seat member of an active team.
func (s *Service) seatTarget(ctx context.Context, project domain.ProjectContext, team domain.AgentTeam, member string) (seatTarget, error) {
	if team.Status != domain.TeamStatusActive {
		return seatTarget{}, domain.NewError(domain.ErrValidation, "the agent team is not active", map[string]any{"team_id": team.ID, "status": team.Status})
	}
	detail, err := s.loadTaskDetail(ctx, contract.ProjectSelector{ProjectID: project.ID}, team.TaskID)
	if err != nil {
		return seatTarget{}, err
	}
	task := detail.task
	task.BucketKey = team.BucketKey
	bucket, _ := s.snapshot.For(task).BucketByKey(team.BucketKey)
	roster, role, err := rosterSeat(bucket.Agents, member)
	if err != nil {
		return seatTarget{}, err
	}
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

// planTeam resolves every seat without starting anything; a seat the team
// does not start when it opens is waiting. seats and plans follow
// team.Roles().
func (s *Service) planTeam(task domain.Task, team domain.BucketAgents, status string) (domain.TeamOutcome, []domain.MemberOutcome, []agentlaunch.Plan) {
	adapters := s.snapshot.For(task).AgentAdapters()
	out := domain.TeamOutcome{Status: status, TaskID: task.ID, Bucket: task.BucketKey}
	var seats []domain.MemberOutcome
	var plans []agentlaunch.Plan
	for _, role := range team.Roles() {
		seatStatus := status
		if !team.StartsAt(role.Key) {
			seatStatus = domain.TeamWaiting
		}
		seat, plan := s.planSeat(adapters, role, seatStatus)
		seats, plans = append(seats, seat), append(plans, plan)
	}
	return out, seats, plans
}

// withSeats returns out with seats placed as its lead and members.
func withSeats(out domain.TeamOutcome, seats []domain.MemberOutcome) domain.TeamOutcome {
	out.Members = nil
	for _, seat := range seats {
		placeSeat(&out, seat)
	}
	return out
}

func (s *Service) planSeat(adapters domain.AgentAdapters, role domain.AgentRole, status string) (domain.MemberOutcome, agentlaunch.Plan) {
	seat := domain.MemberOutcome{Key: role.Key, Status: status, Command: role.Command, Persona: s.commandPersona(role.Command)}
	plan, fallback, perr := s.launcher.Resolve(adapters, role)
	seat.Harness, seat.Model, seat.Fallback, seat.Error = plan.Harness, plan.Model, fallback, perr
	switch {
	case perr != nil && perr.Code == agentUnconfigured:
		seat.Status = domain.TeamUnconfigured
		perr.PreferencesPath, _ = config.PreferencesPath()
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

// spawnTeam records the team and starts, in parallel, the seats it starts
// when it opens. The team fails when none of them starts.
func (s *Service) spawnTeam(ctx context.Context, project domain.ProjectContext, task domain.Task, team domain.BucketAgents) domain.TeamOutcome {
	out, seats, plans := s.planTeam(task, team, domain.TeamStarted)
	fail := func(perr *domain.AgentError) domain.TeamOutcome {
		out.Status, out.Error = domain.TeamFailed, perr
		if perr.Code == agentUnconfigured {
			out.Status = domain.TeamUnconfigured
		}
		return withSeats(out, seats)
	}
	var starts []int
	var unresolved *domain.AgentError
	for i, seat := range seats {
		switch {
		case seat.Status == domain.TeamWaiting:
		case seat.Error != nil:
			unresolved = cmp.Or(unresolved, seat.Error)
		default:
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 {
		return fail(unresolved)
	}
	if perr := s.spawnRefusal(ctx, project, s.snapshot.For(task).AgentAdapters(), len(team.Members), len(starts)); perr != nil {
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
	roles := team.Roles()
	var wg sync.WaitGroup
	for _, i := range starts {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			seats[i] = s.openSeat(ctx, project, task, opened.ID, team, roles[i], plans[i], seats[i])
		}(i)
	}
	wg.Wait()
	out.Follow = teamFollow(opened.ID, task.ID)
	for _, i := range starts {
		if seats[i].Error == nil {
			return withSeats(out, seats)
		}
	}
	_ = s.repo.FailAgentTeam(ctx, opened.ID)
	return fail(seats[starts[0]].Error)
}

// spawnRefusal returns why seats more sessions must not start for a team
// of members: an uninheritable store or a preference limit.
func (s *Service) spawnRefusal(ctx context.Context, project domain.ProjectContext, adapters domain.AgentAdapters, members, seats int) *domain.AgentError {
	if s.spawnBlocked != "" {
		return &domain.AgentError{Code: "store_not_inheritable", Message: s.spawnBlocked}
	}
	if adapters.MaxMembers > 0 && members > adapters.MaxMembers {
		return &domain.AgentError{Code: "max_members", Message: fmt.Sprintf("the team has %d members; preferences allow %d", members, adapters.MaxMembers)}
	}
	if adapters.MaxSessions > 0 {
		s.reconcileSessions(ctx, project)
		running, err := s.repo.CountRunningAgentSessions(ctx, project.ID)
		if err != nil {
			return &domain.AgentError{Code: "store_error", Message: err.Error()}
		}
		if running+seats > adapters.MaxSessions {
			return &domain.AgentError{Code: "max_sessions", Message: fmt.Sprintf("%d sessions are running; preferences allow %d", running, adapters.MaxSessions)}
		}
	}
	return nil
}

// openSeat records the seat's session and its start, writes its prompt,
// and starts its supervisor. A seat that cannot start finishes failed at
// once.
func (s *Service) openSeat(ctx context.Context, project domain.ProjectContext, task domain.Task, teamID string, team domain.BucketAgents, role domain.AgentRole, plan agentlaunch.Plan, seat domain.MemberOutcome) domain.MemberOutcome {
	seat.SessionID = newAgentID("s")
	files, err := agentlaunch.Files(seat.SessionID)
	if err != nil {
		seat.Status, seat.Error = domain.SessionFailed, &domain.AgentError{Code: "start_failed", Message: err.Error()}
		return seat
	}
	seat.Transcript = files.Transcript
	session := domain.AgentSession{ID: seat.SessionID, TeamID: teamID, Member: role.Key, Command: role.Command, Persona: seat.Persona, Harness: plan.Harness, Model: plan.Model, Transcript: files.Transcript, Status: domain.SessionRunning}
	if err := s.repo.AddAgentSession(ctx, session); err != nil {
		seat.Status, seat.Error = domain.SessionFailed, &domain.AgentError{Code: "store_error", Message: err.Error()}
		return seat
	}
	// Recorded before the supervisor starts, so its finish always follows.
	payload := map[string]any{"team_id": teamID, "session_id": session.ID, "member": role.Key, "task_id": task.ID, "harness": plan.Harness, "model": plan.Model, "transcript": files.Transcript}
	if body, err := json.Marshal(payload); err == nil {
		_ = s.repo.RecordEntityEvent(ctx, domain.EventEntityTask, task.ID, project.ID, domain.EventTypeAgentSessionStarted, string(body))
	}
	if err := s.startSeat(ctx, project, task, teamID, team, role, plan, files); err != nil {
		s.finishSession(ctx, project.ID, task.ID, session, domain.SessionFailed, nil, err.Error())
		seat.Status, seat.Error = domain.SessionFailed, &domain.AgentError{Code: "start_failed", Message: err.Error()}
	}
	return seat
}

// startSeat writes the seat's prompt and starts its supervisor.
func (s *Service) startSeat(ctx context.Context, project domain.ProjectContext, task domain.Task, teamID string, team domain.BucketAgents, role domain.AgentRole, plan agentlaunch.Plan, files agentlaunch.SessionFiles) error {
	if err := s.writeSeatPrompt(ctx, files.Prompt, task, teamID, team, role, files.Session); err != nil {
		return err
	}
	storeDir, err := paths.DataDir()
	if err != nil {
		return err
	}
	return s.launcher.Start(ctx, plan, agentlaunch.Request{
		Project: project.Slug,
		Cwd:     project.RootPath,
		Files:   files,
		Vars:    map[string]string{"session_id": files.Session, "team_id": teamID, "member": role.Key, "store_dir": storeDir},
		Env:     agentEnv(files.Session, teamID, role.Key, plan),
	})
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

// writeSeatPrompt writes the seat's prompt file: the resolved command, the
// task, and the team protocol.
func (s *Service) writeSeatPrompt(ctx context.Context, path string, task domain.Task, teamID string, team domain.BucketAgents, role domain.AgentRole, sessionID string) error {
	command, err := s.ResolveCommand(ctx, contract.ResolveCommandInput{Name: role.Command})
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(command.Markdown)
	fmt.Fprintf(&b, "\n\n## Omakiten task\n\nYou work on task #%d %q in bucket %q. Read it with `okt task continue %d`.\n", task.ID, task.Title, task.BucketKey, task.ID)
	fmt.Fprintf(&b, "\n## Your team seat\n\n- Team: `%s`. Your key: `%s`. Your session: `%s`.\n", teamID, role.Key, sessionID)
	fmt.Fprintf(&b, "- You run once: do the work through the board, then exit. Your exit code is your session's status.\n")
	if role.Brief != "" {
		fmt.Fprintf(&b, "- Brief: %s\n", role.Brief)
	}
	if role.Key == domain.AgentLeadKey {
		var keys []string
		for _, m := range team.Members {
			keys = append(keys, "`"+m.Key+"`")
		}
		if len(keys) > 0 {
			fmt.Fprintf(&b, "- You lead members %s. Read each `%s<key>` result, waiting for the ones still missing, then decide: `okt wait --task %d --tag '%s*'`.\n", strings.Join(keys, ", "), domain.AgentResultTag, task.ID, domain.AgentResultTag)
		}
	} else {
		fmt.Fprintf(&b, "- Publish your result exactly once: `okt comment add %d -a agent -T %s%s -b \"…\"`.\n", task.ID, domain.AgentResultTag, role.Key)
	}
	fmt.Fprintf(&b, "- Leave a question for a teammate: `okt comment add %d -a agent -T %s<key> -b \"…\"`. Read the ones left for you: `okt comment list %d --tag %s%s`.\n", task.ID, domain.AgentAddressTag, task.ID, domain.AgentAddressTag, role.Key)
	fmt.Fprintf(&b, "- See the team: `okt team show %s`.\n", teamID)
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// RunAgentSession is the supervisor of one session: it holds the session's
// lock, runs argv until it exits, its timeout passes, or a stop signal
// ends ctx, and records the harness pid and how the run ended. A session
// stopped before its harness started is stopped at once. It runs only
// what agent.spawn started, so it has no surface of its own.
func (s *Service) RunAgentSession(ctx context.Context, input contract.RunAgentSessionInput) (contract.RunAgentSessionResponse, error) {
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.RunAgentSessionResponse{}, err
	}
	session, err := s.repo.GetAgentSession(ctx, strings.TrimSpace(input.SessionID))
	if err != nil {
		return contract.RunAgentSessionResponse{}, err
	}
	team, err := s.projectTeam(ctx, project, session.TeamID)
	if err != nil {
		return contract.RunAgentSessionResponse{}, err
	}
	files, err := agentlaunch.Files(session.ID)
	if err != nil {
		return contract.RunAgentSessionResponse{}, err
	}
	lock, err := agentlaunch.Hold(ctx, files)
	if err != nil {
		return contract.RunAgentSessionResponse{}, err
	}
	defer lock.Release()
	// The run ends on its own terms; recording it must not end with ctx.
	record := context.WithoutCancel(ctx)
	result, runErr := agentlaunch.Supervise(ctx, agentlaunch.Run{
		Argv: input.Argv, Transcript: files.Transcript, Timeout: input.Timeout, Lock: lock,
		Started: func(pid int) {
			_ = s.repo.SetAgentSessionPID(record, session.ID, pid)
			if current, err := s.repo.GetAgentSession(record, session.ID); err == nil && current.Status != domain.SessionRunning {
				_ = s.launcher.Stop(pid)
			}
		},
	})
	status, code, message := domain.SessionDone, &result.ExitCode, ""
	switch {
	case runErr != nil:
		status, code, message = domain.SessionFailed, nil, runErr.Error()
	case result.Stopped:
		status, message = domain.SessionStopped, "stopped by a signal"
	case result.TimedOut:
		status, message = domain.SessionFailed, fmt.Sprintf("timed out after %s", input.Timeout)
	case result.ExitCode != 0:
		status, message = domain.SessionFailed, fmt.Sprintf("exit status %d", result.ExitCode)
	}
	s.finishRun(record, project.ID, team.TaskID, session, status, code, message)
	finished, err := s.repo.GetAgentSession(record, session.ID)
	if err != nil {
		return contract.RunAgentSessionResponse{}, err
	}
	return contract.RunAgentSessionResponse{Project: projectSummary(project), Session: finished}, nil
}

// finishRun finishes a session whose harness ran, then records
// team.finished when it was the last one running in its team.
func (s *Service) finishRun(ctx context.Context, projectID, taskID int64, session domain.AgentSession, status string, exitCode *int, message string) {
	if !s.finishSession(ctx, projectID, taskID, session, status, exitCode, message) {
		return
	}
	team, err := s.repo.GetAgentTeam(ctx, session.TeamID)
	if err != nil {
		return
	}
	payload := map[string]any{"team_id": team.ID, "task_id": team.TaskID, "bucket": team.BucketKey}
	counts := map[string]int{domain.SessionDone: 0, domain.SessionFailed: 0}
	for _, ended := range team.Sessions {
		if _, ok := counts[ended.Status]; ok {
			counts[ended.Status]++
		}
	}
	payload["done"], payload["failed"] = counts[domain.SessionDone], counts[domain.SessionFailed]
	if body, err := json.Marshal(payload); err == nil {
		_ = s.repo.RecordEntityEvent(ctx, domain.EventEntityTask, team.TaskID, projectID, domain.EventTypeTeamFinished, string(body))
	}
}

// finishSession records how a running session ended and emits
// agent.session_finished; a session that already ended is left alone. It
// reports whether the session's team is left active with nothing running.
func (s *Service) finishSession(ctx context.Context, projectID, taskID int64, session domain.AgentSession, status string, exitCode *int, message string) bool {
	finished, idle, err := s.repo.FinishAgentSession(ctx, session.ID, status, exitCode, message)
	if err != nil || !finished {
		return false
	}
	payload := map[string]any{"session_id": session.ID, "team_id": session.TeamID, "member": session.Member, "task_id": taskID, "status": status}
	if exitCode != nil {
		payload["exit_code"] = *exitCode
	}
	if message != "" {
		payload["error"] = message
	}
	if body, err := json.Marshal(payload); err == nil {
		_ = s.repo.RecordEntityEvent(ctx, domain.EventEntityTask, taskID, projectID, domain.EventTypeAgentSessionFinished, string(body))
	}
	return idle
}

// stopSession stops the harness of a running seat. A run whose lock no
// one holds is over, and its pid may name another process.
func (s *Service) stopSession(session domain.AgentSession) {
	if session.Status == domain.SessionRunning && session.PID > 0 && s.launcher.Alive(session.ID) {
		_ = s.launcher.Stop(session.PID)
	}
}

// reconcileSessions finishes, as failed, every running session of the
// project whose run ended without its supervisor recording it: the machine
// restarted or the supervisor was killed.
func (s *Service) reconcileSessions(ctx context.Context, project domain.ProjectContext) {
	sessions, err := s.repo.StaleRunningAgentSessions(ctx, project.ID, orphanGrace)
	if err != nil {
		return
	}
	for _, stale := range sessions {
		if !s.launcher.Alive(stale.Session.ID) {
			s.finishRun(ctx, project.ID, stale.TaskID, stale.Session, domain.SessionFailed, nil, "orphaned: the run ended without its supervisor recording it")
		}
	}
}

func runningTeam(team domain.AgentTeam) domain.TeamOutcome {
	out := domain.TeamOutcome{Status: domain.TeamRunning, TeamID: team.ID, TaskID: team.TaskID, Bucket: team.BucketKey}
	for _, session := range team.Sessions {
		if session.Status != domain.SessionReplaced {
			placeSeat(&out, sessionOutcome(session))
		}
	}
	out.Follow = teamFollow(team.ID, team.TaskID)
	return out
}

// sessionOutcome reports a recorded session as a seat outcome.
func sessionOutcome(session domain.AgentSession) domain.MemberOutcome {
	return domain.MemberOutcome{Key: session.Member, SessionID: session.ID, Status: session.Status, Command: session.Command, Persona: session.Persona, Harness: session.Harness, Model: session.Model, Transcript: session.Transcript}
}

func teamFollow(teamID string, taskID int64) []string {
	id := strconv.FormatInt(taskID, 10)
	return []string{"okt team show " + teamID, "okt wait --task " + id + " --tag '" + domain.AgentResultTag + "*'"}
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
	s.reconcileSessions(ctx, project)
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
	s.reconcileSessions(ctx, project)
	team, err := s.projectTeam(ctx, project, strings.TrimSpace(input.TeamID))
	if err != nil {
		return contract.ShowTeamResponse{}, err
	}
	comments, err := s.newCommentService().List(ctx, project, team.TaskID)
	if err != nil {
		return contract.ShowTeamResponse{}, err
	}
	results, missing := []contract.TeamResult{}, []string{}
	for _, session := range team.Sessions {
		if session.Status == domain.SessionReplaced {
			continue
		}
		published := false
		for _, c := range comments {
			if hasTag(c.Tags, domain.AgentResultTag+session.Member) {
				results = append(results, contract.TeamResult{Member: session.Member, CommentID: c.ID, CreatedAt: c.CreatedAt})
				published = true
			}
		}
		if session.Status == domain.SessionDone && session.Member != domain.AgentLeadKey && !published {
			missing = append(missing, session.Member)
		}
	}
	id := strconv.FormatInt(team.TaskID, 10)
	return contract.ShowTeamResponse{Project: projectSummary(project), Team: team, Results: results, MissingResults: missing,
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
	s.reconcileSessions(ctx, project)
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
