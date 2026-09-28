package entitydetail

import (
	"fmt"
	"strings"

	"omakiten/internal/tui/screenfixture"
	"omakiten/internal/tui/screenhost"
)

const entityDetailGoldenSourcePath = "/home/omakiten/.local/share/omakiten/config/laws/no-silent-failure.md"

func testPayload(kind Kind) Payload {
	return Payload{
		Kind: kind, Slug: "alpha", Header: strings.ToUpper(string(kind)) + " · alpha",
		Rows:      []Row{{Label: "Slug", Value: "alpha"}, {Label: "Source", Value: "/tmp/alpha.md"}},
		BodyLabel: "BODY", Body: strings.Repeat("A detailed body line.\n", 12), Extra: "Press a to set default",
	}
}

func entityDetailGoldenLongPayload(kind Kind) Payload {
	var body strings.Builder
	body.WriteString("The entity body has to survive a paragraph long enough that it wraps at every recorded width, because the wrap column is the first thing a layout migration moves.\n\n")
	for i := 1; i <= 60; i++ {
		fmt.Fprintf(&body, "%02d. body line %02d\n", i, i)
	}
	return Payload{
		Kind:   kind,
		Slug:   "no-silent-failure",
		Header: strings.ToUpper(string(kind)) + " · no-silent-failure",
		Rows: []Row{
			{Label: "Slug", Value: "no-silent-failure"},
			{Label: "Description", Value: "A failure that is swallowed is a failure that is repeated, so every error path either handles the error or returns it to a caller that can."},
			{Label: "Source", Value: entityDetailGoldenSourcePath},
		},
		BodyLabel: "BODY",
		Body:      body.String(),
		Extra:     "Press e to edit this entity in $EDITOR",
	}
}

func entityDetailGoldenBuild(payload Payload) func(screenhost.Frame) screenhost.Screen {
	return func(frame screenhost.Frame) screenhost.Screen {
		return screenfixture.Enter(New().Open(payload), frame)
	}
}

// FixtureScenarios returns every recorded state for the settings entity detail
// screen.
func FixtureScenarios() []screenfixture.Scenario {
	out := make([]screenfixture.Scenario, 0, 6)
	for _, kind := range []Kind{KindLaw, KindPersona, KindSkill, KindTemplate} {
		payload := testPayload(kind)
		out = append(out, screenfixture.Scenario{
			Name:  string(kind),
			Build: entityDetailGoldenBuild(payload),
		})
	}
	out = append(out,
		screenfixture.Scenario{
			Name:  "law-scrolled",
			Build: entityDetailGoldenBuild(entityDetailGoldenLongPayload(KindLaw)),
			Keys:  []string{"pgdown", "j", "j", "j", "j"},
		},
		screenfixture.Scenario{
			Name:  "template-raw-bottom",
			Build: entityDetailGoldenBuild(entityDetailGoldenLongPayload(KindTemplate)),
			Keys:  []string{"M", "G"},
		},
	)
	return out
}
