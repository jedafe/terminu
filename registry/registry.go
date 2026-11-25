// Ref: .teos/sections/01 — builtin command manifests live at repo-root
// registry/ as YAML, embedded into the binary like migrations/.
package registry

import "embed"

// FS is the curated v1 registry — <tool>.yaml multi-doc streams.
//
//go:embed *.yaml
var FS embed.FS
