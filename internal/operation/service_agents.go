package operation

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"omakiten/internal/activity"
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

// closeTeam closes an active team, stops each running seat but the
// caller's own, and records team.closed with reason.
func (s *Service) closeTeam(ctx context.Context, project domain.ProjectContext, team domain.AgentTeam, reason string) (domain.AgentTeam, error) {
	// A seat closing its own team, by a move or team close, runs on: its
	// supervisor records how it ends.
	_, _, _, caller, _ := activity.FromContext(ctx)
	closed, err := s.repo.CloseAgentTeam(ctx, team.ID, caller)
	if err != nil {
		return domain.AgentTeam{}, err
	}
	if !closed {
		return domain.AgentTeam{}, domain.NewError(domain.ErrValidation, "the agent team is not active", map[string]any{"team_id": team.ID, "status": team.Status})
	}
	for _, session := range team.Sessions {
		if session.ID != caller {
			s.stopSession(session)
		}
	}
	s.recordTeamEvent(ctx, team, domain.EventTypeTeamClosed, map[string]any{"reason": reason})
	return s.repo.GetAgentTeam(ctx, team.ID)
}

// recordTeamEvent records eventType on the team's subject, its plan or its
// task, with payload naming the team, the subject, and a task team's bucket.
func (s *Service) recordTeamEvent(ctx context.Context, team domain.AgentTeam, eventType string, payload map[string]any) {
	payload["team_id"] = team.ID
	entity, id := domain.EventEntityTask, team.TaskID
	if team.PlanID > 0 {
		entity, id = domain.EventEntityPlan, team.PlanID
		payload["plan_id"] = id
	} else {
		payload["task_id"], payload["bucket"] = id, team.BucketKey
	}
	if body, err := json.Marshal(payload); err == nil {
		_ = s.repo.RecordEntityEvent(ctx, entity, id, team.ProjectID, eventType, string(body))
	}
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
	if strings.TrimSpace(input.TeamID) != "" || strings.TrimSpace(input.Plan) != "" {
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

// respawnMember runs one seat again in a new session: a roster seat of the
// active team TeamID, or the plan seat of Plan, opening the plan's team
// when it has none. The seat's previous session is stopped and becomes
// replaced.
func (s *Service) respawnMember(ctx context.Context, input contract.SpawnAgentsInput) (contract.SpawnAgentsResponse, error) {
	teamID, slug := strings.TrimSpace(input.TeamID), strings.TrimSpace(input.Plan)
	if strings.TrimSpace(input.Member) == "" || strings.TrimSpace(input.Command) != "" || input.TaskID != 0 || (teamID != "") == (slug != "") {
		return contract.SpawnAgentsResponse{}, domain.NewError(domain.ErrValidation, "--team or --plan runs one seat again: pass one of them with --member, and no --task or --command", nil)
	}
	project, err := s.resolveProject(ctx, input.ProjectSelector)
	if err != nil {
		return contract.SpawnAgentsResponse{}, err
	}
	var team domain.AgentTeam
	if slug != "" {
		var plan domain.Plan
		if plan, err = s.newPlanService().GetBySlug(ctx, project, slug); err == nil {
			team, err = s.openPlanTeam(ctx, project, plan)
		}
	} else {
		team, err = s.projectTeam(ctx, project, teamID)
	}
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

// SpawnSeat starts one seat unless it already has a session: a roster seat
// of the active team TeamID names, or of the active team of TaskID, or the
// plan seat of PlanID. It is what the spawn hook action runs, so a repeated
// event starts nothing new.
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
	} else if input.PlanID > 0 {
		var plan domain.Plan
		if plan, err = s.repo.GetPlanByID(ctx, project.ID, input.PlanID); err == nil {
			team, err = s.openPlanTeam(ctx, project, plan)
		}
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
	out := domain.TeamOutcome{Status: domain.TeamStarted, TeamID: team.ID, TaskID: team.TaskID, PlanID: team.PlanID, Bucket: team.BucketKey, Follow: target.subject.follow(team)}
	old := teamSeat(team, role.Key)
	if old.ID != "" && !replace {
		out.Status = domain.TeamRunning
		placeSeat(&out, sessionOutcome(old))
		return contract.SpawnAgentsResponse{Project: projectSummary(target.project), Team: out}, nil
	}
	adapters := s.snapshot.For(target.subject.task).AgentAdapters()
	seat, plan := s.planSeat(adapters, role, domain.TeamStarted)
	if seat.Error == nil {
		seat.Error = s.spawnRefusal(adapters, target.subject.members)
	}
	var reserved reservedSeat
	if seat.Error == nil {
		reserved, seat = s.seatSession(team.ID, role, plan, seat)
	}
	if seat.Error == nil {
		seat.Error = s.reserveSeats(ctx, target.project, adapters, []reservedSeat{reserved}, old.ID)
	}
	if seat.Error == nil {
		s.stopSession(old)
		seat = s.launchSeat(ctx, target.project, team, target.subject, role, plan, reserved, seat)
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

// seatSubject is what a seat works on: a task, with the bucket team it sits
// in, or a plan. members counts the seats besides a lead.
type seatSubject struct {
	task    domain.Task
	roster  domain.BucketAgents
	plan    *domain.PlanShow
	members int
}

// follow returns the commands that follow a seat of team.
func (s seatSubject) follow(team domain.AgentTeam) []string {
	if s.plan != nil {
		return []string{"okt team show " + team.ID, "okt plan continue " + s.plan.Plan.Slug}
	}
	return teamFollow(team.ID, team.TaskID)
}

// seatTarget is the active team, subject, and role a seat start runs.
type seatTarget struct {
	project domain.ProjectContext
	team    domain.AgentTeam
	subject seatSubject
	role    domain.AgentRole
}

// openPlanTeam returns the active team of an active plan, opening it when
// the plan has none.
func (s *Service) openPlanTeam(ctx context.Context, project domain.ProjectContext, plan domain.Plan) (domain.AgentTeam, error) {
	if plan.Status != domain.PlanStatusActive {
		return domain.AgentTeam{}, domain.NewError(domain.ErrValidation, "the plan is not active", map[string]any{"plan": plan.Slug, "status": plan.Status})
	}
	team, _, err := s.repo.OpenAgentTeam(ctx, domain.AgentTeam{ID: newAgentID("t"), ProjectID: project.ID, PlanID: plan.ID})
	return team, err
}

// seatTarget resolves the seat member of an active team: a roster seat of
// its bucket, or a seat of the workflow's plans block for a plan's team.
func (s *Service) seatTarget(ctx context.Context, project domain.ProjectContext, team domain.AgentTeam, member string) (seatTarget, error) {
	if team.Status != domain.TeamStatusActive {
		return seatTarget{}, domain.NewError(domain.ErrValidation, "the agent team is not active", map[string]any{"team_id": team.ID, "status": team.Status})
	}
	if team.PlanID > 0 {
		return s.planSeatTarget(ctx, project, team, member)
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
	return seatTarget{project: project, team: team, subject: seatSubject{task: task, roster: roster, members: len(roster.Members)}, role: role}, nil
}

// planSeatTarget resolves the plan seat member of a plan's active team.
func (s *Service) planSeatTarget(ctx context.Context, project domain.ProjectContext, team domain.AgentTeam, member string) (seatTarget, error) {
	plan, err := s.repo.GetPlanByID(ctx, project.ID, team.PlanID)
	if err != nil {
		return seatTarget{}, err
	}
	if plan.Status != domain.PlanStatusActive {
		return seatTarget{}, domain.NewError(domain.ErrValidation, "the plan is not active", map[string]any{"plan": plan.Slug, "status": plan.Status})
	}
	seats := s.snapshot.Workflow().Plans.Agents
	role, ok := s.snapshot.Workflow().Plans.Agent(member)
	if !ok {
		keys := make([]string, len(seats))
		for i, seat := range seats {
			keys[i] = seat.Key
		}
		return seatTarget{}, domain.NewError(domain.ErrValidation, "member is not a plan seat of the workflow", map[string]any{"member": member, "plan_seats": keys})
	}
	show, err := s.newPlanService().Show(ctx, project, plan.Slug)
	if err != nil {
		return seatTarget{}, err
	}
	return seatTarget{project: project, team: team, subject: seatSubject{plan: &show, members: len(seats)}, role: role}, nil
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
	seat := domain.MemberOutcome{Key: role.Key, Status: status, Command: role.Command, Persona: cmp.Or(role.Persona, s.commandPersona(role.Command))}
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
	starts, unresolved := startableSeats(seats)
	if len(starts) == 0 {
		return fail(unresolved)
	}
	adapters := s.snapshot.For(task).AgentAdapters()
	if perr := s.spawnRefusal(adapters, len(team.Members)); perr != nil {
		return fail(perr)
	}
	opened, created, err := s.repo.OpenAgentTeam(ctx, domain.AgentTeam{ID: newAgentID("t"), ProjectID: project.ID, TaskID: task.ID, BucketKey: task.BucketKey})
	if err != nil {
		return fail(&domain.AgentError{Code: "store_error", Message: err.Error()})
	}
	if !created {
		return runningTeam(opened)
	}
	out.TeamID, out.Follow = opened.ID, teamFollow(opened.ID, task.ID)
	if perr := s.launchTeam(ctx, project, task, team, opened, seats, plans, starts); perr != nil {
		_ = s.repo.FailAgentTeam(ctx, opened.ID)
		return fail(perr)
	}
	return withSeats(out, seats)
}

// startableSeats returns the indexes of the seats a team starts when it
// opens and can resolve, and the first resolve error of the others.
func startableSeats(seats []domain.MemberOutcome) ([]int, *domain.AgentError) {
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
	return starts, unresolved
}

// launchTeam reserves the starting seats of a new team in one write, then
// launches them in parallel, updating seats. It returns why the team
// fails: the reservation's refusal, or the first error when no seat
// started.
func (s *Service) launchTeam(ctx context.Context, project domain.ProjectContext, task domain.Task, team domain.BucketAgents, opened domain.AgentTeam, seats []domain.MemberOutcome, plans []agentlaunch.Plan, starts []int) *domain.AgentError {
	roles := team.Roles()
	subject := seatSubject{task: task, roster: team, members: len(team.Members)}
	reserved := make([]reservedSeat, len(seats))
	var sessions []reservedSeat
	for _, i := range starts {
		if reserved[i], seats[i] = s.seatSession(opened.ID, roles[i], plans[i], seats[i]); seats[i].Error == nil {
			sessions = append(sessions, reserved[i])
		}
	}
	if perr := s.reserveSeats(ctx, project, s.snapshot.For(task).AgentAdapters(), sessions, ""); perr != nil {
		return perr
	}
	var wg sync.WaitGroup
	for _, i := range starts {
		if seats[i].Error == nil {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				seats[i] = s.launchSeat(ctx, project, opened, subject, roles[i], plans[i], reserved[i], seats[i])
			}(i)
		}
	}
	wg.Wait()
	for _, i := range starts {
		if seats[i].Error == nil {
			return nil
		}
	}
	return seats[starts[0]].Error
}

// spawnRefusal returns why no seat of a team of members may start: an
// uninheritable store or the max_members preference.
func (s *Service) spawnRefusal(adapters domain.AgentAdapters, members int) *domain.AgentError {
	if s.spawnBlocked != "" {
		return &domain.AgentError{Code: "store_not_inheritable", Message: s.spawnBlocked}
	}
	if adapters.MaxMembers > 0 && members > adapters.MaxMembers {
		return &domain.AgentError{Code: "max_members", Message: fmt.Sprintf("the team has %d members; preferences allow %d", members, adapters.MaxMembers)}
	}
	return nil
}

// reservedSeat is a seat's session before it starts, with its files.
type reservedSeat struct {
	session domain.AgentSession
	files   agentlaunch.SessionFiles
}

// reserveSeats finishes the project's orphaned sessions, then records
// seats as running sessions, replacing the session replaced names when
// set, in one write that holds max_sessions.
func (s *Service) reserveSeats(ctx context.Context, project domain.ProjectContext, adapters domain.AgentAdapters, seats []reservedSeat, replaced string) *domain.AgentError {
	s.reconcileSessions(ctx, project)
	sessions := make([]domain.AgentSession, len(seats))
	for i, seat := range seats {
		sessions[i] = seat.session
	}
	err := s.repo.ReserveAgentSessions(ctx, project.ID, sessions, replaced, adapters.MaxSessions)
	var limit domain.SessionLimitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &limit):
		return &domain.AgentError{Code: "max_sessions", Message: limit.Error()}
	default:
		return &domain.AgentError{Code: "store_error", Message: err.Error()}
	}
}

// seatSession names a new session for a seat, with its files.
func (s *Service) seatSession(teamID string, role domain.AgentRole, plan agentlaunch.Plan, seat domain.MemberOutcome) (reservedSeat, domain.MemberOutcome) {
	seat.SessionID = newAgentID("s")
	files, err := agentlaunch.Files(seat.SessionID)
	if err != nil {
		seat.Status, seat.Error = domain.SessionFailed, &domain.AgentError{Code: "start_failed", Message: err.Error()}
		return reservedSeat{}, seat
	}
	seat.Transcript = files.Transcript
	session := domain.AgentSession{ID: seat.SessionID, TeamID: teamID, Member: role.Key, Command: role.Command, Persona: seat.Persona, Harness: plan.Harness, Model: plan.Model, Transcript: files.Transcript, Status: domain.SessionRunning}
	return reservedSeat{session: session, files: files}, seat
}

// launchSeat records a reserved seat's start, writes its prompt, and
// starts its supervisor. A seat that cannot start finishes failed at once.
func (s *Service) launchSeat(ctx context.Context, project domain.ProjectContext, team domain.AgentTeam, subject seatSubject, role domain.AgentRole, plan agentlaunch.Plan, reserved reservedSeat, seat domain.MemberOutcome) domain.MemberOutcome {
	session, files := reserved.session, reserved.files
	// Recorded before the supervisor starts, so its finish always follows.
	s.recordTeamEvent(ctx, team, domain.EventTypeAgentSessionStarted, map[string]any{"session_id": session.ID, "member": role.Key, "harness": plan.Harness, "model": plan.Model, "transcript": files.Transcript})
	if err := s.startSeat(ctx, project, subject, team.ID, role, plan, files); err != nil {
		s.finishSession(ctx, team, session, domain.SessionFailed, nil, err.Error())
		seat.Status, seat.Error = domain.SessionFailed, &domain.AgentError{Code: "start_failed", Message: err.Error()}
	}
	return seat
}

// startSeat writes the seat's prompt and starts its supervisor.
func (s *Service) startSeat(ctx context.Context, project domain.ProjectContext, subject seatSubject, teamID string, role domain.AgentRole, plan agentlaunch.Plan, files agentlaunch.SessionFiles) error {
	if err := s.writeSeatPrompt(files.Prompt, subject, teamID, role, files.Session); err != nil {
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
// task or the plan the seat works on, and the seat's protocol.
func (s *Service) writeSeatPrompt(path string, subject seatSubject, teamID string, role domain.AgentRole, sessionID string) error {
	command, err := s.resolveCommand(role.Command, nil, role.Persona)
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(command.Markdown)
	if subject.plan != nil {
		writePlanSeat(&b, *subject.plan, teamID, role, sessionID)
	} else {
		writeTaskSeat(&b, subject.task, subject.roster, teamID, role, sessionID)
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// writeSeatRun writes the lines every seat's protocol opens with.
func writeSeatRun(b *strings.Builder, heading, teamID string, role domain.AgentRole, sessionID string) {
	fmt.Fprintf(b, "\n## %s\n\n- Team: `%s`. Your key: `%s`. Your session: `%s`.\n", heading, teamID, role.Key, sessionID)
	fmt.Fprintf(b, "- You run once: do the work through the board, then exit. Your exit code is your session's status.\n")
	if role.Brief != "" {
		fmt.Fprintf(b, "- Brief: %s\n", role.Brief)
	}
}

// writeTaskSeat writes the task and the team protocol of a bucket team's
// seat.
func writeTaskSeat(b *strings.Builder, task domain.Task, team domain.BucketAgents, teamID string, role domain.AgentRole, sessionID string) {
	fmt.Fprintf(b, "\n\n## Omakiten task\n\nYou work on task #%d %q in bucket %q. Read it with `okt task continue %d`.\n", task.ID, task.Title, task.BucketKey, task.ID)
	writeSeatRun(b, "Your team seat", teamID, role, sessionID)
	if role.Key == domain.AgentLeadKey {
		var keys []string
		for _, m := range team.Members {
			keys = append(keys, "`"+m.Key+"`")
		}
		if len(keys) > 0 {
			fmt.Fprintf(b, "- You lead members %s. Read each `%s<key>` result, waiting for the ones still missing, then decide: `okt wait --task %d --tag '%s*'`.\n", strings.Join(keys, ", "), domain.AgentResultTag, task.ID, domain.AgentResultTag)
		}
	} else {
		fmt.Fprintf(b, "- Publish your result exactly once: `okt comment add %d -a agent -T %s%s -b \"…\"`.\n", task.ID, domain.AgentResultTag, role.Key)
	}
	fmt.Fprintf(b, "- Leave a question for a teammate: `okt comment add %d -a agent -T %s<key> -b \"…\"`. Read the ones left for you: `okt comment list %d --tag %s%s`.\n", task.ID, domain.AgentAddressTag, task.ID, domain.AgentAddressTag, role.Key)
	fmt.Fprintf(b, "- See the team: `okt team show %s`.\n", teamID)
}

// writePlanSeat writes the plan, its goal, waves, and tasks, and the
// protocol of a plan seat.
func writePlanSeat(b *strings.Builder, show domain.PlanShow, teamID string, role domain.AgentRole, sessionID string) {
	plan := show.Plan
	fmt.Fprintf(b, "\n\n## Omakiten plan\n\nYou work on plan `%s` %q: %d of %d tasks done. Read it with `okt plan continue %s`.\n", plan.Slug, plan.Name, show.DoneCount, show.TotalCount, plan.Slug)
	if goal := strings.TrimSpace(plan.GoalBody); goal != "" {
		fmt.Fprintf(b, "\n### Goal\n\n%s\n", goal)
	}
	if len(show.Waves) > 0 {
		b.WriteString("\n### Waves\n")
		for i, wave := range show.Waves {
			fmt.Fprintf(b, "\n%d. %s: %d of %d done\n", i+1, wave.Wave.Name, wave.DoneCount, wave.TotalCount)
			for _, task := range wave.Tasks {
				fmt.Fprintf(b, "   - #%d %q in `%s`\n", task.TaskID, task.Title, task.BucketKey)
			}
		}
	}
	writeSeatRun(b, "Your plan seat", teamID, role, sessionID)
	fmt.Fprintf(b, "- Publish your result on the plan exactly once: `okt comment add --scope plan --plan %s -a agent -T %s%s -b \"…\"`.\n", plan.Slug, domain.AgentResultTag, role.Key)
	fmt.Fprintf(b, "- See the seat: `okt team show %s`.\n", teamID)
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
	s.finishRun(record, team, session, status, code, message)
	finished, err := s.repo.GetAgentSession(record, session.ID)
	if err != nil {
		return contract.RunAgentSessionResponse{}, err
	}
	return contract.RunAgentSessionResponse{Project: projectSummary(project), Session: finished}, nil
}

// finishRun finishes a session whose harness ran, then records
// team.finished when it was the last one running in its team.
func (s *Service) finishRun(ctx context.Context, team domain.AgentTeam, session domain.AgentSession, status string, exitCode *int, message string) {
	if !s.finishSession(ctx, team, session, status, exitCode, message) {
		return
	}
	team, err := s.repo.GetAgentTeam(ctx, session.TeamID)
	if err != nil {
		return
	}
	counts := map[string]int{domain.SessionDone: 0, domain.SessionFailed: 0}
	for _, ended := range team.Sessions {
		if _, ok := counts[ended.Status]; ok {
			counts[ended.Status]++
		}
	}
	s.recordTeamEvent(ctx, team, domain.EventTypeTeamFinished, map[string]any{"done": counts[domain.SessionDone], "failed": counts[domain.SessionFailed]})
}

// finishSession records how a running session ended and emits
// agent.session_finished; a session that already ended is left alone. It
// reports whether the session's team is left active with nothing running.
func (s *Service) finishSession(ctx context.Context, team domain.AgentTeam, session domain.AgentSession, status string, exitCode *int, message string) bool {
	finished, idle, err := s.repo.FinishAgentSession(ctx, session.ID, status, exitCode, message)
	if err != nil || !finished {
		return false
	}
	payload := map[string]any{"session_id": session.ID, "member": session.Member, "status": status}
	if exitCode != nil {
		payload["exit_code"] = *exitCode
	}
	if message != "" {
		payload["error"] = message
	}
	s.recordTeamEvent(ctx, team, domain.EventTypeAgentSessionFinished, payload)
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
			s.finishRun(ctx, stale.Team, stale.Session, domain.SessionFailed, nil, "orphaned: the run ended without its supervisor recording it")
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
	follow, err := s.teamSubjectFollow(ctx, project, team)
	if err != nil {
		return contract.ShowAgentResponse{}, err
	}
	resp := contract.ShowAgentResponse{Project: projectSummary(project), Session: session, TaskID: team.TaskID, PlanID: team.PlanID, Bucket: team.BucketKey,
		Follow: []string{follow[0], "okt logs --session " + session.ID, "okt team show " + team.ID}}
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
	comments, err := s.teamComments(ctx, project, team)
	if err != nil {
		return contract.ShowTeamResponse{}, err
	}
	follow, err := s.teamSubjectFollow(ctx, project, team)
	if err != nil {
		return contract.ShowTeamResponse{}, err
	}
	results, missing := teamResults(team, comments)
	return contract.ShowTeamResponse{Project: projectSummary(project), Team: team, Results: results, MissingResults: missing, Follow: follow}, nil
}

// teamResults returns the review-<key> comments the team's seats published
// and the members whose session ended done without one.
func teamResults(team domain.AgentTeam, comments []domain.Comment) ([]contract.TeamResult, []string) {
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
	return results, missing
}

// teamComments returns the comments of the team's subject: its plan's, or
// its task's.
func (s *Service) teamComments(ctx context.Context, project domain.ProjectContext, team domain.AgentTeam) ([]domain.Comment, error) {
	if team.PlanID > 0 {
		return s.newCommentService().Query(ctx, project, domain.CommentFilter{Scope: domain.CommentScopePlan, ProjectID: project.ID, PlanID: team.PlanID})
	}
	return s.newCommentService().List(ctx, project, team.TaskID)
}

// teamSubjectFollow returns the commands that read the team's subject and
// wait for its results.
func (s *Service) teamSubjectFollow(ctx context.Context, project domain.ProjectContext, team domain.AgentTeam) ([]string, error) {
	if team.PlanID > 0 {
		plan, err := s.repo.GetPlanByID(ctx, project.ID, team.PlanID)
		if err != nil {
			return nil, err
		}
		return []string{"okt plan continue " + plan.Slug, "okt comment list --scope plan --plan " + plan.Slug}, nil
	}
	id := strconv.FormatInt(team.TaskID, 10)
	return []string{"okt task continue " + id, "okt wait --task " + id + " --tag '" + domain.AgentResultTag + "*'"}, nil
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
