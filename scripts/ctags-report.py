#!/usr/bin/env python3
"""
Parse a Universal Ctags file and produce a concise codebase report for LLM consumption.

Designed to distill ~10k+ tag entries into a structured summary that fits
comfortably in an LLM context window (~2-4k tokens by default).

Usage:
    python scripts/ctags-report.py                  # compact report (default)
    python scripts/ctags-report.py --verbose         # full detail for every section
    python scripts/ctags-report.py --top 30          # show top-30 files instead of 20
    python scripts/ctags-report.py --section hooks   # only the React hooks section
    python scripts/ctags-report.py -o report.md      # write to file
"""

from __future__ import annotations

import argparse
import os
import sys
from collections import Counter, defaultdict
from dataclasses import dataclass, field


# ── Ctags kind-code mapping (Universal Ctags TypeScript + CSS) ──────────────

KIND_LABELS = {
    "C": "constant",
    "G": "generator",
    "a": "alias",
    "c": "class",
    "e": "enumerator",
    "f": "function",
    "g": "enum",
    "i": "interface",
    "m": "method",
    "n": "namespace",
    "p": "property",
    "v": "variable",
    "s": "selector",  # CSS
}

# Short abbreviations for compact breakdown strings
KIND_ABBREV = {
    "C": "const", "p": "prop", "f": "fn", "m": "meth",
    "i": "iface", "c": "cls", "a": "alias", "v": "var",
    "e": "enumv", "g": "enum", "G": "gen", "n": "ns", "s": "sel",
}


@dataclass
class Tag:
    name: str
    filepath: str  # relative to project root
    pattern: str
    kind: str  # single-char kind code
    scope: str = ""  # e.g. "class:MyComponent" or "enum:NotificationType"


@dataclass
class FileStats:
    path: str
    kinds: Counter = field(default_factory=Counter)
    symbols: list[Tag] = field(default_factory=list)

    @property
    def total(self) -> int:
        return sum(self.kinds.values())


# ── Parsing ─────────────────────────────────────────────────────────────────

def parse_tags(tags_path: str, project_root: str) -> list[Tag]:
    """Parse a Universal Ctags file, stripping the project root from paths."""
    tags: list[Tag] = []
    root_prefix = project_root.rstrip("/") + "/"

    with open(tags_path, "r", encoding="utf-8", errors="replace") as fh:
        for line in fh:
            if line.startswith("!"):
                continue
            parts = line.rstrip("\n").split("\t")
            if len(parts) < 4:
                continue

            name = parts[0]
            raw_path = parts[1]
            pattern = parts[2]

            # Kind is the first single-char field after the pattern
            kind = ""
            scope = ""
            for extra in parts[3:]:
                stripped = extra.rstrip(';"').strip()
                if len(stripped) == 1 and stripped.isalpha():
                    kind = stripped
                elif ":" in stripped and not stripped.startswith("/"):
                    scope = stripped

            # Make path relative
            rel_path = raw_path
            if raw_path.startswith(root_prefix):
                rel_path = raw_path[len(root_prefix):]

            tags.append(Tag(name=name, filepath=rel_path, pattern=pattern, kind=kind, scope=scope))

    return tags


def detect_project_root(tags_path: str) -> str:
    """Read !_TAG_PROC_CWD from the tags file to find the project root."""
    with open(tags_path, "r") as fh:
        for line in fh:
            if line.startswith("!_TAG_PROC_CWD"):
                parts = line.split("\t")
                if len(parts) >= 2:
                    return parts[1].strip().rstrip("/")
            if not line.startswith("!"):
                break
    return os.path.dirname(tags_path)


# ── Classification ──────────────────────────────────────────────────────────

def classify_directory(path: str) -> str:
    """Classify a file path into a broad area."""
    if path.startswith("supabase/functions/"):
        parts = path.split("/")
        if len(parts) >= 3:
            fn_name = parts[2]
            if fn_name == "_shared":
                return "supabase/shared"
            return f"supabase/fn:{fn_name}"
        return "supabase/functions"
    if path.startswith("src/components/"):
        parts = path.split("/")
        if len(parts) >= 3:
            return f"components/{parts[2]}"
        return "components"
    if path.startswith("src/hooks/"):
        return "hooks"
    if path.startswith("src/services/"):
        return "services"
    if path.startswith("src/utils/"):
        return "utils"
    if path.startswith("src/pages/"):
        return "pages"
    if path.startswith("src/contexts/") or path.startswith("src/context/"):
        return "contexts"
    if path.startswith("src/integrations/"):
        return "integrations"
    if path.startswith("src/styles/") or path.endswith(".css"):
        return "styles"
    if path.startswith("src/"):
        return "src/other"
    return "other"


def is_css_only(tag: Tag) -> bool:
    """Return True if the tag is from a CSS file (noise for code analysis)."""
    return tag.filepath.endswith(".css")


# ── Index construction ──────────────────────────────────────────────────────

def build_index(tags: list[Tag]):
    """Pre-compute all the indexes used by report sections."""
    kind_counts = Counter(t.kind for t in tags)
    file_stats: dict[str, FileStats] = {}
    area_counts: Counter = Counter()
    area_kind_counts: dict[str, Counter] = defaultdict(Counter)
    members_by_scope: dict[str, list[Tag]] = defaultdict(list)

    for t in tags:
        if t.filepath not in file_stats:
            file_stats[t.filepath] = FileStats(path=t.filepath)
        fs = file_stats[t.filepath]
        fs.kinds[t.kind] += 1
        fs.symbols.append(t)

        area = classify_directory(t.filepath)
        area_counts[area] += 1
        area_kind_counts[area][t.kind] += 1

        if t.scope:
            members_by_scope[t.scope].append(t)

    return kind_counts, file_stats, area_counts, area_kind_counts, members_by_scope


# ── Report sections ─────────────────────────────────────────────────────────

def _kind_breakdown_str(counter: Counter, keys: str = "fciGamep") -> str:
    """Compact breakdown like '48fn, 1cls, 109iface'."""
    parts = []
    for k in keys:
        if counter[k] > 0:
            parts.append(f"{counter[k]}{KIND_ABBREV.get(k, k)}")
    return ", ".join(parts)


def section_summary(tags, kind_counts, file_stats, **_) -> list[str]:
    lines = ["# Codebase Report (from ctags)", ""]
    lines.append(f"**{len(tags):,}** symbols across **{len(file_stats)}** files")
    lines.append("")
    lines.append("## Symbol breakdown")
    lines.append("")
    for kind_code, count in kind_counts.most_common():
        label = KIND_LABELS.get(kind_code, kind_code)
        lines.append(f"  {label:12s}  {count:>5,}")
    lines.append("")
    return lines


def section_areas(area_counts, area_kind_counts, **_) -> list[str]:
    lines = ["## Areas (by symbol count)", ""]
    for area, count in area_counts.most_common(30):
        breakdown = area_kind_counts[area]
        note = _kind_breakdown_str(breakdown)
        note_str = f" ({note})" if note else ""
        lines.append(f"  {area:40s}  {count:>4}{note_str}")
    lines.append("")
    return lines


def section_top_files(file_stats, top_n=20, **_) -> list[str]:
    lines = [f"## Top {top_n} files by symbol count", ""]
    sorted_files = sorted(file_stats.values(), key=lambda fs: fs.total, reverse=True)
    for fs in sorted_files[:top_n]:
        bd = _kind_breakdown_str(fs.kinds, "CpfmicavegG")
        bd_str = f" [{bd}]" if bd else ""
        lines.append(f"  {fs.total:>4}  {fs.path}{bd_str}")
    lines.append("")
    return lines


def section_classes(tags, members_by_scope, verbose=False, **_) -> list[str]:
    """Classes & interfaces. Compact: skip CSS, show classes + interfaces with members."""
    items = [t for t in tags if t.kind in ("c", "i") and not is_css_only(t)]
    items.sort(key=lambda t: t.filepath)

    # Annotate with member count for sorting/filtering
    annotated = []
    for t in items:
        members = (members_by_scope.get(f"class:{t.name}", [])
                   + members_by_scope.get(f"interface:{t.name}", []))
        annotated.append((t, members))

    omitted = 0
    if not verbose:
        # Compact: show all classes + top 30 interfaces by member count
        classes = [(t, m) for t, m in annotated if t.kind == "c"]
        interfaces = [(t, m) for t, m in annotated if t.kind == "i"]
        interfaces.sort(key=lambda x: -len(x[1]))
        top_ifaces = interfaces[:30]
        annotated = classes + top_ifaces
        omitted = len(interfaces) - len(top_ifaces)

    lines = [f"## Classes & interfaces ({len(items)} total, showing {len(annotated)})", ""]

    for t, members in annotated:
        kind_label = "class" if t.kind == "c" else "iface"
        member_count = len(members)
        suffix = f"  ({member_count} members)" if member_count else ""
        lines.append(f"  {kind_label:6s}  {t.name:40s}  {t.filepath}{suffix}")
        if verbose and members:
            for m in members[:20]:
                mlabel = KIND_ABBREV.get(m.kind, m.kind)
                lines.append(f"           .{m.name} ({mlabel})")
            if len(members) > 20:
                lines.append(f"           ... and {len(members) - 20} more")
    if not verbose and omitted > 0:
        lines.append(f"  ... and {omitted} more interfaces (use --verbose to see all)")
    lines.append("")
    return lines


def section_enums(tags, members_by_scope, **_) -> list[str]:
    enums = [t for t in tags if t.kind == "g"]
    if not enums:
        return []
    lines = ["## Enums", ""]
    for t in enums:
        vals = members_by_scope.get(f"enum:{t.name}", [])
        val_names = [v.name for v in vals[:10]]
        val_str = " = " + ", ".join(val_names) if val_names else ""
        if len(vals) > 10:
            val_str += f", ... (+{len(vals) - 10})"
        lines.append(f"  {t.name:40s}  {t.filepath}{val_str}")
    lines.append("")
    return lines


def section_aliases(tags, verbose=False, **_) -> list[str]:
    aliases = [t for t in tags if t.kind == "a"]
    if not aliases:
        return []
    lines = [f"## Type aliases ({len(aliases)})", ""]
    if not verbose:
        # Compact: group by file, show counts
        by_file: dict[str, list[str]] = defaultdict(list)
        for t in aliases:
            by_file[t.filepath].append(t.name)
        for fpath in sorted(by_file):
            names = by_file[fpath]
            lines.append(f"  {fpath}: {', '.join(names)}")
    else:
        for t in aliases:
            lines.append(f"  {t.name:40s}  {t.filepath}")
    lines.append("")
    return lines


def section_functions(tags, verbose=False, **_) -> list[str]:
    """Standalone functions (not methods). Compact: top files by fn count. Verbose: every name."""
    functions = [t for t in tags if t.kind == "f" and not t.scope]
    functions.sort(key=lambda t: t.filepath)

    by_file: dict[str, list[str]] = defaultdict(list)
    for t in functions:
        by_file[t.filepath].append(t.name)

    if not verbose:
        # Show files sorted by function count, cap at top 25
        lines = [f"## Standalone functions ({len(functions)} across {len(by_file)} files)", ""]
        sorted_files = sorted(by_file.items(), key=lambda x: -len(x[1]))
        shown = sorted_files[:25]
        for fpath, names in shown:
            names_str = ", ".join(names[:6])
            if len(names) > 6:
                names_str += f", ... (+{len(names) - 6})"
            lines.append(f"  {fpath}  ({len(names)})")
            lines.append(f"    {names_str}")
        if len(sorted_files) > 25:
            remaining_files = len(sorted_files) - 25
            remaining_fns = sum(len(n) for _, n in sorted_files[25:])
            lines.append(f"  ... and {remaining_files} more files with {remaining_fns} functions")
    else:
        lines = [f"## Standalone functions ({len(functions)} total)", ""]
        for fpath in sorted(by_file):
            names = by_file[fpath]
            lines.append(f"  # {fpath}")
            for name in names:
                lines.append(f"    {name}")
    lines.append("")
    return lines


def section_hooks(tags, verbose=False, **_) -> list[str]:
    """React hooks (use* pattern). Compact: names only. Verbose: with file paths."""
    hook_map: dict[str, set[str]] = defaultdict(set)
    for t in tags:
        if t.kind in ("f", "C", "v") and t.name.startswith("use") and t.name[3:4].isupper():
            hook_map[t.name].add(t.filepath)

    if not hook_map:
        return []

    hook_names = sorted(hook_map)
    lines = [f"## React hooks ({len(hook_names)})", ""]

    if verbose:
        for h in hook_names:
            files = sorted(hook_map[h])
            lines.append(f"  {h:45s}  {', '.join(files)}")
    else:
        # Compact: list names, 4 per line
        for i in range(0, len(hook_names), 4):
            chunk = hook_names[i:i + 4]
            lines.append("  " + "  ".join(f"{h:24s}" for h in chunk))
    lines.append("")
    return lines


def section_edge_functions(tags, **_) -> list[str]:
    """Supabase edge functions."""
    fn_dirs = sorted(set(
        t.filepath.split("/")[2]
        for t in tags
        if t.filepath.startswith("supabase/functions/") and len(t.filepath.split("/")) >= 3
    ))
    fn_dirs = [d for d in fn_dirs if d != "_shared"]
    if not fn_dirs:
        return []

    lines = [f"## Supabase edge functions ({len(fn_dirs)})", ""]
    for fn_dir in fn_dirs:
        fn_tags = [t for t in tags if t.filepath.startswith(f"supabase/functions/{fn_dir}/")]
        fn_funcs = [t.name for t in fn_tags if t.kind == "f"]
        fn_str = f"  fns: {', '.join(fn_funcs[:5])}" if fn_funcs else ""
        if len(fn_funcs) > 5:
            fn_str += f", ... (+{len(fn_funcs) - 5})"
        lines.append(f"  {fn_dir:45s}  {len(fn_tags):>3} syms{fn_str}")
    lines.append("")
    return lines


# ── Report assembly ─────────────────────────────────────────────────────────

SECTIONS = {
    "summary": section_summary,
    "areas": section_areas,
    "files": section_top_files,
    "classes": section_classes,
    "enums": section_enums,
    "aliases": section_aliases,
    "functions": section_functions,
    "hooks": section_hooks,
    "edge-functions": section_edge_functions,
}


def build_report(
    tags: list[Tag],
    top_n: int = 20,
    verbose: bool = False,
    sections: list[str] | None = None,
) -> str:
    kind_counts, file_stats, area_counts, area_kind_counts, members_by_scope = build_index(tags)

    ctx = dict(
        tags=tags,
        kind_counts=kind_counts,
        file_stats=file_stats,
        area_counts=area_counts,
        area_kind_counts=area_kind_counts,
        members_by_scope=members_by_scope,
        top_n=top_n,
        verbose=verbose,
    )

    chosen = sections if sections else list(SECTIONS.keys())
    all_lines: list[str] = []
    for name in chosen:
        fn = SECTIONS.get(name)
        if fn:
            all_lines.extend(fn(**ctx))

    return "\n".join(all_lines)


# ── CLI ─────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(
        description="Generate a codebase report from ctags",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="Sections: " + ", ".join(SECTIONS.keys()),
    )
    parser.add_argument("tags_file", nargs="?", default="tags",
                        help="Path to ctags file (default: ./tags)")
    parser.add_argument("--top", type=int, default=20,
                        help="Number of top files to show (default: 20)")
    parser.add_argument("--verbose", action="store_true",
                        help="Full detail for every section")
    parser.add_argument("--section", action="append", dest="sections",
                        choices=list(SECTIONS.keys()),
                        help="Only include specific section(s) — repeatable")
    parser.add_argument("--project-root", default=None,
                        help="Project root to strip from paths (auto-detected)")
    parser.add_argument("-o", "--output", default=None,
                        help="Write report to file instead of stdout")
    args = parser.parse_args()

    tags_path = args.tags_file
    if not os.path.isabs(tags_path):
        tags_path = os.path.join(os.getcwd(), tags_path)

    if not os.path.exists(tags_path):
        print(f"Error: tags file not found at {tags_path}", file=sys.stderr)
        print("Run scripts/ctags-gen.sh first to generate it.", file=sys.stderr)
        sys.exit(1)

    project_root = args.project_root or detect_project_root(tags_path)
    tags = parse_tags(tags_path, project_root)
    report = build_report(tags, top_n=args.top, verbose=args.verbose, sections=args.sections)

    if args.output:
        with open(args.output, "w") as fh:
            fh.write(report)
        print(f"Report written to {args.output} ({len(report):,} chars)", file=sys.stderr)
    else:
        print(report)


if __name__ == "__main__":
    main()
