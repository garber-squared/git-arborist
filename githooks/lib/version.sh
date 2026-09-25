# Shared helpers for the version hooks. The version is derived from the commit
# count on master: 0.<n/10>.<n%10> (41 commits -> 0.4.1), and is stored in
# internal/version/VERSION so every build (make or plain `go build`) embeds it.

VERSION_FILE=internal/version/VERSION

# version_for <count> prints the version string for a master commit count.
version_for() {
	echo "0.$(($1 / 10)).$(($1 % 10))"
}

# version_at <rev> prints the VERSION recorded in <rev>, or nothing.
version_at() {
	git show "$1:$VERSION_FILE" 2>/dev/null | tr -d '[:space:]'
}

# write_version <version> writes the VERSION file in the current worktree.
write_version() {
	echo "$1" > "$(git rev-parse --show-toplevel)/$VERSION_FILE"
}
