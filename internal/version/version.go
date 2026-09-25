// Package version holds the build version of arborist.
package version

import (
	_ "embed"
	"strings"
)

// VERSION is derived from the number of commits on master:
// 0.<n/10>.<n%10>, so every commit to master bumps it (40 commits -> 0.4.0,
// 41 -> 0.4.1). The githooks/ pre-push and post-merge hooks keep it current.
//
//go:embed VERSION
var raw string

var Version = strings.TrimSpace(raw)
