package plan

// Percent is a wave's completion as an integer percentage, floored. A wave
// with no tasks is 0% rather than a division by zero.
func Percent(done, total int) int {
	if total <= 0 {
		return 0
	}
	return done * 100 / total
}
