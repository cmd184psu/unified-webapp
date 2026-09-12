// apply.go implements FR-M3/FR-M4's live-apply pipeline: validate a
// candidate config.AuthConfig with boot's exact rule set, splice it into the
// on-disk config file leaving every other byte untouched, and swap the
// running auth.Service onto a freshly built Policy -- all with no restart
// and no partially-applied state.
package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"cmd184psu/unified-webapp/internal/platform/auth"
	"cmd184psu/unified-webapp/internal/platform/config"
)

// applyAuth is the sole entry point for a live admin edit of auth.* config
// (T5.4's handlers all funnel through this). It:
//
//  0. Expands every non-empty Modules[...].PinFile (including the
//     modules.admin.pin_file spelling) relative to the config file's
//     directory (expandModulePinFiles) -- mirroring config.Load's
//     expandAuthPaths, which runs only at boot. Without this, a relative
//     pin_file submitted through the live-edit API would be validated (and
//     persisted) relative to the server process's CWD instead of the config
//     file's location, diverging from what the very same value would mean
//     on the next restart.
//  1. Validates the expanded config with auth.ValidatePolicy, passing
//     h.deps.KnownModules and h.deps.AdminRouted -- the exact same function
//     and arguments boot uses (security-plan.md L12) -- so a live edit can
//     never accept something boot would have refused, or vice versa. A
//     rejection returns the error and changes nothing anywhere: no file
//     write, no swap.
//  2. Builds a fresh *auth.Policy from the expanded config with
//     auth.BuildPolicy.
//  3. Splices the expanded config into h.deps.ConfigPath in place
//     (spliceAuthConfig), atomically (tmp file + rename) so the file is
//     never observed partial. The persisted pin_file is therefore always the
//     expanded absolute form -- the same value GET /api/config/auth reports
//     and the same value boot would produce from it.
//  4. Swaps the running Service onto the new Policy (auth.Service.SwapPolicy),
//     taking effect on the very next request with no restart.
//
// Ordering deviation from the plan's literal validate -> write -> swap
// sequence: BuildPolicy (step 2 above) runs *before* the file write, not
// between the write and the swap. ValidatePolicy and BuildPolicy both take
// the same config.AuthConfig; a BuildPolicy failure right after a passing
// ValidatePolicy would mean the two are inconsistent with each other, which
// should never happen in practice. But if it ever did, doing BuildPolicy
// first means that failure leaves the on-disk file (and the live policy)
// completely untouched -- strictly stronger than the plan's literal
// ordering, which would leave a freshly-spliced file on disk with no
// corresponding swap. The externally observable contract is identical
// either way: reject changes nothing, accept changes the file and the live
// policy together.
//
// The returned config.AuthConfig is newAuth after path expansion -- callers
// (mutateAuth) must store this, not the caller's original newAuth, so the
// Handler's in-memory copy (and therefore every subsequent GET) reflects the
// same expanded paths that were validated and persisted.
func (h *Handler) applyAuth(newAuth config.AuthConfig) (config.AuthConfig, error) {
	expanded, err := expandModulePinFiles(newAuth, filepath.Dir(h.deps.ConfigPath))
	if err != nil {
		return config.AuthConfig{}, err
	}

	if err := auth.ValidatePolicy(expanded, h.deps.KnownModules, h.deps.AdminRouted); err != nil {
		return config.AuthConfig{}, err
	}

	p, err := auth.BuildPolicy(expanded)
	if err != nil {
		return config.AuthConfig{}, err
	}

	if err := spliceAuthConfig(h.deps.ConfigPath, expanded); err != nil {
		return config.AuthConfig{}, err
	}

	h.deps.Service.SwapPolicy(p)
	return expanded, nil
}

// expandModulePinFiles returns a copy of a with every non-empty
// Modules[...].PinFile (including the modules.admin.pin_file spelling of the
// admin PIN source) made absolute relative to configDir via
// config.ExpandRelativeTo -- the same treatment config.Load's expandAuthPaths
// gives every module's pin_file at boot, reproduced here so a live-apply
// resolves a relative path against the config file's directory rather than
// the process's working directory. a itself is never mutated; an empty or
// nil Modules map is returned unchanged.
func expandModulePinFiles(a config.AuthConfig, configDir string) (config.AuthConfig, error) {
	if len(a.Modules) == 0 {
		return a, nil
	}
	modules := make(map[string]config.ModuleAuthConfig, len(a.Modules))
	for key, m := range a.Modules {
		if m.PinFile != "" {
			expanded, err := config.ExpandRelativeTo(m.PinFile, configDir)
			if err != nil {
				return config.AuthConfig{}, fmt.Errorf("auth.modules.%s.pin_file: %w", key, err)
			}
			m.PinFile = expanded
		}
		modules[key] = m
	}
	a.Modules = modules
	return a, nil
}

// authMemberLocation describes where the top-level "auth" member lives (or
// would be inserted) in a config file's bytes, as located by
// locateAuthMember.
type authMemberLocation struct {
	// found is true when a top-level "auth" member exists.
	found bool

	// valueStart/valueEnd bound the "auth" member's *value* only (not its
	// key or the surrounding colon/whitespace/comma) when found is true.
	valueStart, valueEnd int64

	// insertAt is the byte offset at which a new "auth" member must be
	// inserted when found is false: immediately after the preceding
	// member's value (so a comma can be appended right after it), or
	// immediately after the top-level "{" when the object has no members
	// at all.
	insertAt int64

	// needsComma is true when insertAt sits right after an existing
	// member's value (so the inserted text must start with a comma before
	// the new member); false only for a wholly empty top-level object.
	needsComma bool

	// indent is the whitespace prefix used by the file's other top-level
	// members (captured from "auth"'s own line when found, otherwise from
	// the first depth-1 member encountered). Falls back to two spaces when
	// no depth-1 member spans a line break the indentation can be read
	// from (e.g. a fully compact/minified file).
	indent string
}

// locateAuthMember walks data -- the raw bytes of a JSON config file -- with
// a json.Decoder, reading only the top-level object's immediate (depth-1)
// members. Token() is used solely to read each member's key; each member's
// *value*, whatever its own internal nesting, is consumed in one call via
// Decode(&json.RawMessage{}), which both (a) returns the value's exact
// original bytes verbatim (RawMessage never reformats, sorts, or
// re-escapes) and (b) advances the decoder past the value's entire subtree
// regardless of how deep it nests -- so a key belonging to some nested
// object several levels down is never seen by this loop's Token() call and
// can never be mistaken for the top-level "auth" member. This is how
// nesting depth is handled correctly: only real depth-1 keys are ever
// tokenized as keys at all.
//
// dec.InputOffset() is precise to the byte for both Token() and Decode()
// (encoding/json's scanner tracks exact stream position, not read-buffer
// boundaries), so valueStart is recovered as valueEnd-len(raw) rather than
// needing a separate "start of value" offset.
func locateAuthMember(data []byte) (authMemberLocation, error) {
	dec := json.NewDecoder(bytes.NewReader(data))

	tok, err := dec.Token()
	if err != nil {
		return authMemberLocation{}, fmt.Errorf("admin: config is not valid JSON: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return authMemberLocation{}, fmt.Errorf("admin: config root is not a JSON object")
	}
	openBraceEnd := dec.InputOffset()

	var loc authMemberLocation
	var lastValueEnd int64 = -1
	haveIndent := false

	for dec.More() {
		preKeyOffset := dec.InputOffset()
		keyTok, err := dec.Token()
		if err != nil {
			return authMemberLocation{}, fmt.Errorf("admin: reading config: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return authMemberLocation{}, fmt.Errorf("admin: config object has a non-string key")
		}
		keyEndOffset := dec.InputOffset()

		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return authMemberLocation{}, fmt.Errorf("admin: reading config member %q: %w", key, err)
		}
		valueEnd := dec.InputOffset()
		valueStart := valueEnd - int64(len(raw))

		if ind, ok := lineIndent(data, preKeyOffset, keyEndOffset); ok {
			if !haveIndent {
				loc.indent = ind
				haveIndent = true
			}
			if key == "auth" {
				loc.indent = ind
			}
		}

		if key == "auth" {
			loc.found = true
			loc.valueStart = valueStart
			loc.valueEnd = valueEnd
		}

		lastValueEnd = valueEnd
	}

	if _, err := dec.Token(); err != nil {
		return authMemberLocation{}, fmt.Errorf("admin: reading config: %w", err)
	}

	if !loc.found {
		if lastValueEnd >= 0 {
			loc.insertAt = lastValueEnd
			loc.needsComma = true
		} else {
			loc.insertAt = openBraceEnd
			loc.needsComma = false
		}
		if loc.indent == "" {
			loc.indent = "  "
		}
	}

	return loc, nil
}

// lineIndent reports the whitespace between the last newline in
// data[from:to] and the next '"' after it -- i.e. the indentation a JSON
// object key at that position is written with. from/to are decoder input
// offsets bracketing a member's key token (from is the offset just before
// the key was read, to is the offset just after). ok is false when the key
// is not the first token on its line (e.g. a fully compact/minified file),
// in which case the caller falls back to a default indent.
func lineIndent(data []byte, from, to int64) (string, bool) {
	segment := data[from:to]
	nl := bytes.LastIndexByte(segment, '\n')
	if nl == -1 {
		return "", false
	}
	rest := segment[nl+1:]
	q := bytes.IndexByte(rest, '"')
	if q == -1 {
		return "", false
	}
	return string(rest[:q]), true
}

// spliceAuthConfig rewrites the top-level "auth" member of the JSON config
// file at path so that it marshals newAuth, leaving every other byte of the
// file byte-for-byte untouched (AC-14). A map[string]json.RawMessage
// re-marshal of the whole file is deliberately not used here -- encoding/json
// sorts map keys alphabetically, compacts RawMessage contents, and
// HTML-escapes '<', '>', '&', so re-marshaling the file as a generic map
// would reorder members, reformat every value, and mangle their bytes even
// though RawMessage preserves each value's own content; the only mechanism
// that satisfies AC-14 is a textual splice of exactly the "auth" member's
// range, located by locateAuthMember.
//
// The new content is marshaled with json.MarshalIndent(newAuth, indent,
// "  ") where indent is the file's own top-level member indentation
// (captured by locateAuthMember): passing that indent as MarshalIndent's
// prefix has each line after the first begin with it, which is exactly
// "the marshaled block re-indented to the member's depth by prefixing each
// line" -- MarshalIndent's prefix parameter does that prefixing directly,
// so there is no separate manual re-indent pass.
//
// The rewritten content is written to a tmp file (mode 0600) in the same
// directory as path, then moved into place with os.Rename, so a crash
// between the write and the rename can never leave path holding a partial
// file -- os.Rename within the same filesystem is atomic.
func spliceAuthConfig(path string, newAuth config.AuthConfig) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("admin: reading config: %w", err)
	}

	loc, err := locateAuthMember(data)
	if err != nil {
		return err
	}

	marshaled, err := json.MarshalIndent(newAuth, loc.indent, "  ")
	if err != nil {
		return fmt.Errorf("admin: marshaling auth config: %w", err)
	}

	var out bytes.Buffer
	out.Grow(len(data) + len(marshaled) + 32)

	if loc.found {
		out.Write(data[:loc.valueStart])
		out.Write(marshaled)
		out.Write(data[loc.valueEnd:])
	} else {
		out.Write(data[:loc.insertAt])
		if loc.needsComma {
			out.WriteString(",\n")
		} else {
			out.WriteString("\n")
		}
		out.WriteString(loc.indent)
		out.WriteString(`"auth": `)
		out.Write(marshaled)
		out.Write(data[loc.insertAt:])
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".auth-apply-*.tmp")
	if err != nil {
		return fmt.Errorf("admin: creating temp config file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("admin: setting temp config file permissions: %w", err)
	}
	if _, err := tmp.Write(out.Bytes()); err != nil {
		tmp.Close()
		return fmt.Errorf("admin: writing temp config file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("admin: closing temp config file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("admin: replacing config file: %w", err)
	}

	return nil
}
