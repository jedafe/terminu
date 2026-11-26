// Ref: .teos/conventions/adapter-manifest-format.md, .teos/sections/04
// Command manifest types + YAML decode + validation. The schema is the
// contract — an invalid manifest never reaches the index
// (invalid_manifest, api-response-format).
package registry

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Manifest is one command definition — executable + argv templates only;
// a manifest can never expand to a shell string (Rule 4).
type Manifest struct {
	ID          string      `yaml:"id" json:"id"`
	Application string      `yaml:"application" json:"application"`
	Name        string      `yaml:"name" json:"name"`
	Description string      `yaml:"description" json:"description"`
	Parameters  []Parameter `yaml:"parameters" json:"parameters,omitempty"`
	Command     CommandSpec `yaml:"command" json:"command"`
	Execution   ExecSpec    `yaml:"execution" json:"execution"`
	Context     ContextSpec `yaml:"context" json:"context"`
	Tags        []string    `yaml:"tags" json:"tags,omitempty"`
	Source      string      `yaml:"source" json:"source"`

	// ShadowedBy is set by the resolver — a lower-priority source with
	// the same id stays in the table but leaves the index.
	ShadowedBy string `yaml:"-" json:"shadowed_by,omitempty"`
}

// Parameter is one declared input the argv templates may reference.
type Parameter struct {
	Name        string   `yaml:"name" json:"name"`
	Type        string   `yaml:"type" json:"type"`                   // resource|string|number|boolean|enum
	Resource    string   `yaml:"resource" json:"resource,omitempty"` // provider id for type=resource
	Required    bool     `yaml:"required" json:"required,omitempty"`
	Flag        *string  `yaml:"flag" json:"flag,omitempty"`       // nil positional; "--name" flag form
	Options     []string `yaml:"options" json:"options,omitempty"` // enum values
	Default     any      `yaml:"default" json:"default,omitempty"`
	Description string   `yaml:"description" json:"description,omitempty"`
}

// CommandSpec is the structured invocation.
type CommandSpec struct {
	Executable string   `yaml:"executable" json:"executable"`
	Arguments  []string `yaml:"arguments" json:"arguments"`
}

// ExecSpec is the broker-facing risk + confirmation declaration.
type ExecSpec struct {
	Risk         string `yaml:"risk" json:"risk"`
	Confirmation bool   `yaml:"confirmation" json:"confirmation"`
	TimeoutS     int    `yaml:"timeout_s" json:"timeout_s"`
	WorkingDir   string `yaml:"working_dir" json:"working_dir"`
}

// ContextSpec declares when a command is context-relevant (§04).
type ContextSpec struct {
	Processes []string `yaml:"processes" json:"processes"`
	Files     []string `yaml:"files" json:"files"`
}

// Source priorities — higher wins an id collision, loser marks shadowed.
var sourceRank = map[string]int{"builtin": 3, "custom": 2, "discovered": 1}

// ErrInvalidManifest maps to the catalog's invalid_manifest code.
var ErrInvalidManifest = errors.New("invalid_manifest")

var idRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)
var tmplRe = regexp.MustCompile(`\{([a-zA-Z0-9_.]+)\}`)

var paramTypes = map[string]bool{
	"resource": true, "string": true, "number": true, "boolean": true, "enum": true,
}

var riskClasses = map[string]bool{
	"informational": true, "controlled": true, "confirmed": true, "restricted": true,
}

// DecodeManifests reads a YAML stream — one or more documents separated
// by "---" — into manifests. Every doc must validate; a bad doc fails
// the whole file (curated files are all-or-nothing).
func DecodeManifests(r io.Reader, source string) ([]*Manifest, error) {
	dec := yaml.NewDecoder(r)
	var out []*Manifest
	for {
		var m Manifest
		err := dec.Decode(&m)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: yaml: %v", ErrInvalidManifest, err)
		}
		m.Source = source
		if err := m.Validate(); err != nil {
			return nil, err
		}
		out = append(out, &m)
	}
	return out, nil
}

// Validate enforces the format's hard rules.
func (m *Manifest) Validate() error {
	if m.ID == "" || !idRe.MatchString(m.ID) {
		return fmt.Errorf("%w: id %q must be dotted-lowercase", ErrInvalidManifest, m.ID)
	}
	if m.Application == "" {
		return fmt.Errorf("%w: %s: application required", ErrInvalidManifest, m.ID)
	}
	if m.Name == "" {
		return fmt.Errorf("%w: %s: name required", ErrInvalidManifest, m.ID)
	}
	if m.Command.Executable == "" {
		return fmt.Errorf("%w: %s: command.executable required", ErrInvalidManifest, m.ID)
	}
	if !riskClasses[m.Execution.Risk] {
		return fmt.Errorf("%w: %s: unknown risk class %q", ErrInvalidManifest, m.ID, m.Execution.Risk)
	}
	// risk ≥ controlled implies the confirmation path (format hard rule).
	if m.Execution.Risk != "informational" {
		m.Execution.Confirmation = true
	}
	params := map[string]bool{}
	for _, p := range m.Parameters {
		if p.Name == "" {
			return fmt.Errorf("%w: %s: unnamed parameter", ErrInvalidManifest, m.ID)
		}
		if !paramTypes[p.Type] {
			return fmt.Errorf("%w: %s: param %q bad type %q", ErrInvalidManifest, m.ID, p.Name, p.Type)
		}
		if p.Type == "enum" && len(p.Options) == 0 {
			return fmt.Errorf("%w: %s: param %q enum needs options", ErrInvalidManifest, m.ID, p.Name)
		}
		if p.Type == "resource" && p.Resource == "" {
			return fmt.Errorf("%w: %s: param %q resource needs a provider id", ErrInvalidManifest, m.ID, p.Name)
		}
		// A declared default must coerce — an enum default outside
		// options is a schema bug that should fail at load, not at
		// execute when the param is left blank.
		if p.Default != nil {
			if _, err := coerce(p, p.Default); err != nil {
				return fmt.Errorf("%w: %s: param %q default: %v", ErrInvalidManifest, m.ID, p.Name, err)
			}
		}
		params[p.Name] = true
	}
	// Every template ref must bind to a declared param or a session.*
	// context field — refs resolve as argv values, never text-spliced
	// into a command line (Rule 4).
	check := func(where, arg string) error {
		for _, ref := range tmplRe.FindAllStringSubmatch(arg, -1) {
			root, _, _ := strings.Cut(ref[1], ".")
			if root == "session" {
				if ref[1] != "session.cwd" {
					return fmt.Errorf("%w: %s: %s: unknown session ref {%s}", ErrInvalidManifest, m.ID, where, ref[1])
				}
				continue
			}
			if !params[root] {
				return fmt.Errorf("%w: %s: %s: {%s} not a declared param", ErrInvalidManifest, m.ID, where, ref[1])
			}
			// Field access is {name.id} only — {p.anything} resolving to
			// p's whole value would silently mislead manifest authors.
			if i := strings.Index(ref[1], "."); i >= 0 && ref[1][i+1:] != "id" {
				return fmt.Errorf("%w: %s: %s: {%s}: only {name.id} field access is supported", ErrInvalidManifest, m.ID, where, ref[1])
			}
		}
		return nil
	}
	for i, a := range m.Command.Arguments {
		if err := check(fmt.Sprintf("arguments[%d]", i), a); err != nil {
			return err
		}
	}
	if err := check("working_dir", m.Execution.WorkingDir); err != nil {
		return err
	}
	return nil
}

// coerce renders one param value as its argv string form — load-time
// validation for declared defaults.
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
