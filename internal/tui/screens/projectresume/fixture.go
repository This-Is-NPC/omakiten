package projectresume

import (
	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

func resumeGoldenPayload() Payload {
	return Payload{
		TaskBuckets: []BucketCount{
			{BucketKey: "backlog", Name: "Backlog", Count: 2},
			{BucketKey: "dev", Name: "Development", Count: 1},
		},
		LikelyNextWork: []TaskSummary{
			{ID: 11, Title: "Ship resume UI", BucketKey: "backlog", Priority: "high"},
		},
		BlockedWork: []TaskSummary{
			{ID: 22, Title: "Blocked by deps", BucketKey: "dev"},
		},
		Dependencies: []DependencySummary{
			{TaskID: 22, DependsOnTaskID: 11},
		},
		NextStepPrompt: "Choose a likely next task",
	}
}

// FixtureScenarios is the gallery/fit-gate mount for Project › Resume.
func FixtureScenarios() []screenfixture.Scenario {
	return []screenfixture.Scenario{
		{
			Name: "loaded",
			Build: func(frame screenhost.Frame) screenhost.Screen {
				return screenfixture.Enter(New().Apply(resumeGoldenPayload()), frame)
			},
		},
		{
			Name: "computing",
			Build: func(frame screenhost.Frame) screenhost.Screen {
				return screenfixture.Enter(New(), frame)
			},
		},
	}
}
