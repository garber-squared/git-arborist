// Package version holds the build version of arborist.
package version

// Version is injected at build time by the Makefile via -ldflags. It is
// derived from the number of commits on master: 0.<n/10>.<n%10>, so every
// commit to master bumps it (40 commits -> 0.4.0, 41 -> 0.4.1).
var Version = "dev"
