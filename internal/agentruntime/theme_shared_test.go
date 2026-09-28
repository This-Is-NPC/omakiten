package agentruntime

import (
	"omakiten/internal/contract"
)

func lawPresent(laws []contract.LawInfo, slug string) bool {
	for _, l := range laws {
		if l.Slug == slug {
			return true
		}
	}
	return false
}

func lawSlugs(laws []contract.LawInfo) []string {
	out := make([]string, 0, len(laws))
	for _, l := range laws {
		out = append(out, l.Slug)
	}
	return out
}

func skillSlugs(skills []contract.SkillInfo) []string {
	out := make([]string, 0, len(skills))
	for _, s := range skills {
		out = append(out, s.Slug)
	}
	return out
}
