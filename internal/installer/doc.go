// Package installer implements the post-binary-install setup logic that
// the curl|bash and irm|iex install scripts used to carry inline as
// hundreds of lines of shell. The Go port is the single source of truth
// for: the okt() shell-rc wrapper block (sentinels byte-identical to
// install.sh and uninstall.sh), the supported-harness list, and the
// CSV-ish selection parser shared by the env-var headless path
// (OKT_HARNESSES) and the bubbletea picker.
package installer
