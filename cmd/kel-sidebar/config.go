package main

// kel's config.toml, read and written by the settings screen with bin/kel's
// rules (kel_config_read): `key = value` on any line, sections and comments
// ignored, a quoted value running to its closing quote.

import (
	"os"
	"strconv"
	"path/filepath"
	"strings"

	"github.com/tharpep/kelsier/internal/fleet"
)

// kelConfigPath is the user's config.toml.
func kelConfigPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "kel", "config.toml")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "kel", "config.toml")
}

// parseConfigLine is one line's key and value, or ok false for a blank,
// comment, section or junk line.
func parseConfigLine(l string) (key, val string, ok bool) {
	l = strings.TrimLeft(l, " \t")
	if l == "" || l[0] == '#' || l[0] == '[' || !strings.Contains(l, "=") {
		return "", "", false
	}
	key, val, _ = strings.Cut(l, "=")
	key = strings.TrimSpace(key)
	for _, c := range key {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_') {
			return "", "", false
		}
	}
	val = strings.TrimLeft(val, " \t")
	switch {
	case strings.HasPrefix(val, `"`), strings.HasPrefix(val, `'`):
		q := val[:1]
		val, _, _ = strings.Cut(val[1:], q)
	case strings.HasPrefix(val, "#"):
		val = ""
	default:
		if i := strings.Index(val, " #"); i >= 0 {
			val = val[:i]
		}
		val = strings.TrimSpace(val)
	}
	return key, val, key != ""
}

// readConfig is every key the file sets; the first setting of a key wins, as
// it does in bin/kel.
func readConfig(path string) map[string]string {
	c := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := parseConfigLine(l); ok {
			if _, seen := c[k]; !seen {
				c[k] = v
			}
		}
	}
	return c
}

// setConfig writes key = "val" over the line that sets key, keeping its
// indent and trailing comment, or adds it at the end. A write that fails
// shows as the old value: the screen reads the file back.
func setConfig(path, key, val string) error {
	b, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(b) == 0 {
		lines = nil
	}
	set := key + ` = "` + val + `"`
	done := false
	for i, l := range lines {
		k, _, ok := parseConfigLine(l)
		if !ok || k != key {
			continue
		}
		indent := l[:len(l)-len(strings.TrimLeft(l, " \t"))]
		comment := ""
		if j := strings.LastIndex(l, " #"); j >= 0 && strings.Count(l[:j], `"`)%2 == 0 {
			comment = "  " + strings.TrimSpace(l[j:])
		}
		lines[i] = indent + set + comment
		done = true
		break
	}
	if !done {
		lines = append(lines, set)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	fleet.WriteAtomic(path, []byte(strings.Join(lines, "\n")+"\n"))
	return nil
}

// cacheTTL is cache_ttl: KEL_CACHE_TTL when exported, else the user's
// config, else Claude Code's five minutes. Read on each load, so a change in
// Settings shows on the next one.
func cacheTTL() int64 {
	v := os.Getenv("KEL_CACHE_TTL")
	if v == "" {
		v = readConfig(kelConfigPath())["cache_ttl"]
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n <= 0 {
		return defaultCacheTTL
	}
	return n
}
