// Package scenariosdata embeds the scenario YAML templates so they ship
// inside the binary. The package is consumed by internal/promql.
package scenariosdata

import "embed"

// FS contains every *.yaml file in this directory. The `all:` prefix
// ensures files starting with `_` (like _meta.yaml) are included; the
// loader filters them out by name.
//
//go:embed all:*.yaml
var FS embed.FS
