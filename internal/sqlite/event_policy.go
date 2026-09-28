package sqlite

import (
	"omakiten/internal/config"
)

type projectEventPolicy struct {
	settings    config.EventsSettings
	recentLimit int
	groups      []config.RetentionGroup
	index       map[string]int
}

func newProjectEventPolicy(settings config.EventsSettings, recentLimit int) projectEventPolicy {
	groups := settings.BuildRetentionGroups()
	return projectEventPolicy{settings: settings, recentLimit: recentLimit, groups: groups, index: retentionIndex(groups)}
}

func retentionIndex(groups []config.RetentionGroup) map[string]int {
	index := make(map[string]int, len(groups)*4)
	for i, group := range groups {
		for _, eventType := range group.EventTypes {
			index[eventType] = i
		}
	}
	return index
}

// policyScopeLocked separates explicit project policies from the default policy.
// The caller holds configMu.
func (s *Store) policyScopeLocked(projectID int64) eventRegistryScope {
	if projectID > 0 {
		return eventRegistryScope{projectID: projectID}
	}
	excluded := make([]int64, 0, len(s.projectPolicies))
	for id := range s.projectPolicies {
		if id > 0 {
			excluded = append(excluded, id)
		}
	}
	return eventRegistryScope{excluded: excluded}
}

// eventPolicyLocked resolves an explicit project policy or its default scope.
func (s *Store) eventPolicyLocked(projectID int64) (projectEventPolicy, int64) {
	if policy, ok := s.projectPolicies[projectID]; ok {
		return policy, projectID
	}
	return s.projectPolicies[0], 0
}
