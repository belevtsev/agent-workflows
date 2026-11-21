"""Package the local Kitsu marketplace used by both coding-agent hosts."""
import argparse
from pathlib import Path
from zipfile import ZIP_DEFLATED, ZipFile, ZipInfo


def package(root, output):
    files = [
        ".agents/plugins/marketplace.json", ".claude-plugin/marketplace.json",
        ".claude-plugin/plugin.json", ".codex-plugin/plugin.json",
        "hooks/codex-hooks.json", "hooks/hooks.json", "docs/brand/kitsu-mascot.png", "LICENSE",
        "plugins/kitsu-team/.claude-plugin/plugin.json", "plugins/kitsu-team/.codex-plugin/plugin.json",
    ]
    for pattern in ("skills/**/*.md", "plugins/kitsu-team/agents/*.md", "plugins/kitsu-team/skills/**/*.md"):
        files.extend(p.relative_to(root).as_posix() for p in root.glob(pattern))
    output.parent.mkdir(parents=True, exist_ok=True)
    with ZipFile(output, "w") as archive:
        for name in sorted(files):
            data = (root / name).read_bytes()
            member = ZipInfo("kitsu-marketplace/" + name)
            member.create_system = 3
            member.external_attr = 0o100644 << 16
            archive.writestr(member, data, compress_type=ZIP_DEFLATED)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("output", type=Path)
    package(Path(__file__).resolve().parents[2], parser.parse_args().output)
