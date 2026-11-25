// Ref: .teos/sections/04 (Execution), .teos/conventions/adapter-manifest-format.md
// Param resolution → structured argv. Templates bind to declared params
// and resolve as argv element values — never into a command string
// (Rule 4). The broker gates; this package only builds.
package registry

import (
	"fmt"
	"strconv"
	"strings"
)

// SessionCtx is the execution-time context a manifest may reference —
// today just the calling session's cwd ({session.cwd}).
type SessionCtx struct {
	Cwd string
}

// Build resolves params against the manifest's declared parameters and
// returns executable + argv + working dir. Missing required params and
// type/enum violations fail with ErrInvalidManifest — callers map it to
// bad_request/invalid_manifest.
func (m *Manifest) Build(params map[string]any, sess SessionCtx) (argv []string, cwd string, err error) {
	vals, err := m.resolveParams(params)
	if err != nil {
		return nil, "", err
	}
	argv = append(argv, m.Command.Executable)
	for _, arg := range m.Command.Arguments {
		argv = append(argv, substitute(arg, vals, sess))
	}
	// Flagged params render after the argument templates — a subcommand
	// argv like `git commit` + flag `-m` must land as `git commit -m x`,
	// never `git -m x commit`. POSIX-style parsers take options after
	// positionals, so tail placement is the correct general rule.
	for _, p := range m.Parameters {
		if p.Flag == nil {
			continue
		}
		v, ok := vals[p.Name]
		if !ok {
			continue
		}
		if p.Type == "boolean" {
			if v == "true" {
				argv = append(argv, *p.Flag)
			}
			continue
		}
		argv = append(argv, *p.Flag, v)
	}
	cwd = substitute(m.Execution.WorkingDir, vals, sess)
	return argv, cwd, nil
}

// resolveParams coerces + validates caller-supplied values into strings.
// Positional rendering happens here only as value ordering — flagged
// params re-read vals in Build.
func (m *Manifest) resolveParams(params map[string]any) (map[string]string, error) {
	vals := map[string]string{}
	for _, p := range m.Parameters {
		raw, present := params[p.Name]
		if !present || raw == nil {
			raw = p.Default
		}
		if raw == nil {
			if p.Required {
				return nil, fmt.Errorf("%w: %s: missing required param %q", ErrInvalidManifest, m.ID, p.Name)
			}
			continue
		}
		v, err := coerce(p, raw)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: param %q: %v", ErrInvalidManifest, m.ID, p.Name, err)
		}
		vals[p.Name] = v
	}
	return vals, nil
}

// coerce renders one param value as its argv string form.
func coerce(p Parameter, raw any) (string, error) {
	switch p.Type {
	case "boolean":
		switch v := raw.(type) {
		case bool:
			return strconv.FormatBool(v), nil
		case string:
			return strconv.FormatBool(v == "true" || v == "1" || v == "yes"), nil
		}
		return "", fmt.Errorf("want boolean")
	case "number":
		switch v := raw.(type) {
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64), nil
		case int, int64:
			return fmt.Sprintf("%d", v), nil
		case string:
			if _, err := strconv.ParseFloat(v, 64); err != nil {
				return "", fmt.Errorf("want number")
			}
			return v, nil
		}
		return "", fmt.Errorf("want number")
	case "enum":
		s := fmt.Sprint(raw)
		for _, o := range p.Options {
			if s == o {
				return s, nil
			}
		}
		return "", fmt.Errorf("want one of %s", strings.Join(p.Options, ","))
	case "resource":
		// A resource resolves through its provider — the caller passes
		// the provider's object ({id: ...}) or a bare id string.
		switch v := raw.(type) {
		case string:
			if v == "" {
				return "", fmt.Errorf("empty resource id")
			}
			return v, nil
		case map[string]any:
			if id, ok := v["id"].(string); ok && id != "" {
				return id, nil
			}
		}
		return "", fmt.Errorf("want resource id or {id}")
	default: // string
		s := fmt.Sprint(raw)
		if p.Required && s == "" {
			return "", fmt.Errorf("empty value")
		}
		return s, nil
	}
}

// substitute replaces {param} / {param.field} / {session.X} refs inside
// one template element. Resource objects aren't addressable by field
// beyond {name.id} → the object's "id".
func substitute(tmpl string, vals map[string]string, sess SessionCtx) string {
	return tmplRe.ReplaceAllStringFunc(tmpl, func(ref string) string {
		key := ref[1 : len(ref)-1]
		if key == "session.cwd" {
			return sess.Cwd
		}
		// {name.id} on a scalar resource resolves to the value itself —
		// the coerced form is already the id string.
		if i := strings.Index(key, "."); i >= 0 {
			return vals[key[:i]]
		}
		return vals[key]
	})
}
