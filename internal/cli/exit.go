package cli

type exitError struct {
	code int
}

func (e exitError) Error() string {
	return ""
}

func ExitCode(err error) (int, bool) {
	if e, ok := err.(exitError); ok {
		return e.code, true
	}
	return 0, false
}
