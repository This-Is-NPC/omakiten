package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// confirmOverwrite prompts the user on stderr asking whether to overwrite a
// divergent config file. Returns true only when the answer parses as
// affirmative ("y" or "yes", case-insensitive). When stdin is not a TTY the
// function returns (false, nil) without prompting so non-interactive callers
// fail fast and require an explicit --force.
func confirmOverwrite(in io.Reader, errOut io.Writer, path string) (bool, error) {
	if !stdinIsTerminal(in) {
		return false, nil
	}
	fmt.Fprintf(errOut, "%s already exists and differs from the embedded preset. Overwrite? [y/N] ", path)
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return false, err
		}
		return false, nil
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes", nil
}

func stdinIsTerminal(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
