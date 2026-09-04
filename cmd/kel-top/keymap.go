package main

import (
	_ "embed"
	"fmt"
	"strings"
)

// keymap.tsv is the one file both kel-top and bin/kel read — see its own
// header comment and docs/interface.md §6. go:embed cannot reach outside its
// package's own directory (`//go:embed ../keymap.tsv` fails to compile,
// confirmed against a scratch module rather than assumed), which is the
// entire reason the file lives under cmd/kel-top/ instead of the repo root.
// bin/kel finds this same physical file by walking up from its own resolved
// path (keymap_file() there) — one file, two ways in, neither a copy.
//
//go:embed keymap.tsv
var keymapTSV string

// keyBinding is one row of the keymap: an action, the scope it belongs to,
// and the key each implementation's own idiom binds it to.
//
// Deliberately not bubbles' key.Binding yet. That type ships with the
// bubbles dependency, which interface-build.md's Phase 2 adds — Phase 1
// changes no behaviour and wires nothing into Update() or the footer, so
// there is no consumer here that needs the real type yet. Introducing the
// dependency a phase early, for a struct nothing reads, is the kind of scope
// creep CLAUDE.md's "no runtime, no build" line is bet against.
type keyBinding struct {
	Scope, Action, GoKey, BashKey, Help string
}

// loadKeymap parses the embedded keymap.tsv. Comment (#) and blank lines are
// skipped; every remaining line must have exactly 5 tab-separated fields.
//
// strings.Split on a literal "\t" is not the trap CLAUDE.md's first entry
// warns about: that trap is bash's `IFS=$'\t' read`, where tab counts as IFS
// *whitespace* and read folds runs of it and drops empty fields. Split has no
// such behaviour — a genuinely empty field comes back as "", not shifted into
// the next column — but the keymap still writes "-" for "no key yet" rather
// than leaving a column blank, so a malformed row (wrong field count) is
// loud rather than silently plausible.
func loadKeymap() ([]keyBinding, error) {
	var rows []keyBinding
	for n, line := range strings.Split(keymapTSV, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			return nil, fmt.Errorf("keymap.tsv:%d: expected 5 tab-separated fields, got %d: %q", n+1, len(f), line)
		}
		rows = append(rows, keyBinding{Scope: f[0], Action: f[1], GoKey: f[2], BashKey: f[3], Help: f[4]})
	}
	return rows, nil
}

// dumpKeys renders "scope\taction\tkey" per row, one line each, for the
// parity/collision guard in test/kel-test.sh. It prints kel-top's OWN key
// (go_key) — bin/kel's _dumpkeys prints its own (bash_key) from the same
// file. The two idioms' keys are not expected to match (interface.md §2:
// "Idioms deliberately differ"); the action set — scope+action pairs — is
// what the guard compares.
func dumpKeys(rows []keyBinding) string {
	var b strings.Builder
	for _, r := range rows {
		fmt.Fprintf(&b, "%s\t%s\t%s\n", r.Scope, r.Action, r.GoKey)
	}
	return b.String()
}
