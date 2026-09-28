//go:build ignore

// check-coverage.go enforces the aggregate statement coverage floor.
// It is invoked as a named file so this tool never enters the Go package set.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const floorPercent = 780 // 78.0%, expressed in tenths of a percent.

var profileBlock = regexp.MustCompile(`^([^\s:]+):([1-9][0-9]*)\.([1-9][0-9]*),([1-9][0-9]*)\.([1-9][0-9]*) ([0-9]+) ([0-9]+)$`)
var totalPercent = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?%$`)

type coverageBlock struct {
	statements uint64
	executed   bool
}

func fail(format string, args ...any) error { return fmt.Errorf("coverage: FAIL: "+format, args...) }

func parseProfile(path string) (covered, total uint64, err error) {
	f, e := os.Open(path)
	if e != nil {
		return 0, 0, fail("cannot read profile: %v", e)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	if !s.Scan() {
		return 0, 0, fail("empty profile: %s", path)
	}
	if s.Text() != "mode: set" && s.Text() != "mode: count" && s.Text() != "mode: atomic" {
		return 0, 0, fail("malformed coverage profile header")
	}
	rows := 0
	blocks := map[string]coverageBlock{}
	for s.Scan() {
		line := s.Text()
		m := profileBlock.FindStringSubmatch(line)
		if m == nil {
			return 0, 0, fail("malformed coverage profile")
		}
		// Reject backwards ranges while retaining the complete producer grammar.
		startLine, e1 := strconv.ParseUint(m[2], 10, 64)
		startCol, e2 := strconv.ParseUint(m[3], 10, 64)
		endLine, e3 := strconv.ParseUint(m[4], 10, 64)
		endCol, e4 := strconv.ParseUint(m[5], 10, 64)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			return 0, 0, fail("malformed coverage profile coordinates")
		}
		if endLine < startLine || (endLine == startLine && endCol < startCol) {
			return 0, 0, fail("malformed coverage profile range")
		}
		statements, e1 := strconv.ParseUint(m[6], 10, 64)
		executions, e2 := strconv.ParseUint(m[7], 10, 64)
		if e1 != nil || e2 != nil {
			return 0, 0, fail("malformed coverage profile counts")
		}
		key := strings.Join(m[1:6], ":")
		previous, duplicate := blocks[key]
		if duplicate && previous.statements != statements {
			return 0, 0, fail("conflicting statement counts for coverage block")
		}
		if !duplicate {
			if ^uint64(0)-total < statements {
				return 0, 0, fail("coverage profile statement count overflows")
			}
			total += statements
		}
		if executions > 0 && !previous.executed {
			if ^uint64(0)-covered < statements {
				return 0, 0, fail("coverage profile statement count overflows")
			}
			covered += statements
		}
		blocks[key] = coverageBlock{statements: statements, executed: previous.executed || executions > 0}
		rows++
	}
	if e := s.Err(); e != nil {
		return 0, 0, fail("cannot read profile: %v", e)
	}
	if rows == 0 || total == 0 {
		return 0, 0, fail("coverage profile has no statement blocks")
	}
	return covered, total, nil
}

func validateSummary(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return fail("cannot read summary: %v", e)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	rows := 0
	for s.Scan() {
		line := s.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "total:") {
			rows++
			fields := strings.Fields(line)
			if len(fields) != 3 || fields[0] != "total:" || fields[1] != "(statements)" || !totalPercent.MatchString(fields[2]) {
				return fail("malformed total row")
			}
			ratio, e := strconv.ParseFloat(strings.TrimSuffix(fields[2], "%"), 64)
			if e != nil || ratio < 0 || ratio > 100 {
				return fail("malformed total row")
			}
		}
	}
	if e := s.Err(); e != nil {
		return fail("cannot read summary: %v", e)
	}
	if rows != 1 {
		return fail("summary has no unique total row")
	}
	return nil
}

func newestGo(root string) (int64, error) {
	var newest int64 = -1
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			if path != root {
				name := d.Name()
				if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
					return filepath.SkipDir
				}
				switch name {
				case "bin", "build", "dist", "dev_env", "vendor", "node_modules":
					return filepath.SkipDir
				}
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if n := info.ModTime().UnixNano(); n > newest {
			newest = n
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	if newest < 0 {
		return 0, errors.New("no Go source files")
	}
	return newest, nil
}

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: check-coverage.go PROFILE SUMMARY SOURCE_ROOT")
		os.Exit(1)
	}
	profile, summary, root := os.Args[1], os.Args[2], os.Args[3]
	pinfo, e := os.Stat(profile)
	if e != nil || pinfo.Size() == 0 {
		fmt.Fprintln(os.Stderr, fail("missing or empty profile: %s", profile))
		os.Exit(1)
	}
	sinfo, e := os.Stat(summary)
	if e != nil || sinfo.Size() == 0 {
		fmt.Fprintln(os.Stderr, fail("missing or empty summary: %s", summary))
		os.Exit(1)
	}
	covered, total, e := parseProfile(profile)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	if e = validateSummary(summary); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	newest, e := newestGo(root)
	if e != nil || pinfo.ModTime().UnixNano() < newest || sinfo.ModTime().UnixNano() < newest {
		fmt.Fprintln(os.Stderr, fail("stale coverage profile"))
		os.Exit(1)
	}
	left := new(big.Int).Mul(new(big.Int).SetUint64(covered), big.NewInt(1000))
	right := new(big.Int).Mul(new(big.Int).SetUint64(total), big.NewInt(floorPercent))
	percentage := func() string {
		ratio := new(big.Rat).SetFrac(new(big.Int).SetUint64(covered), new(big.Int).SetUint64(total))
		return new(big.Rat).Mul(ratio, big.NewRat(100, 1)).FloatString(3)
	}
	if left.Cmp(right) < 0 {
		fmt.Fprintf(os.Stderr, "coverage: FAIL: aggregate statement coverage %s%% is below 78.000%%\n", percentage())
		os.Exit(1)
	}
	fmt.Printf("coverage: PASS: aggregate statement coverage %s%% (floor 78.000%%; profile %d/%d)\n", percentage(), covered, total)
}
