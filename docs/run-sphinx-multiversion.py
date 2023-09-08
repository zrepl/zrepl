#!/usr/bin/env python3

from pathlib import Path
import subprocess
import re
import argparse
import distutils

argparser = argparse.ArgumentParser()
argparser.add_argument("docsroot")
argparser.add_argument("outdir")
args = argparser.parse_args()

output = subprocess.run(["git", "tag", "-l"], capture_output=True, check=True, text=True)
tagRE = re.compile(r"^v(?P<major>\d+)\.(?P<minor>\d+)\.(?P<patch>\d+)(-rc(?P<rc>\d+))?$")
class Tag:
    orig: str
    major: int
    minor: int
    patch: int
    rc: int

    def __str__(self):
        return self.orig

    def __repr__(self):
        return self.orig

tags = []
for line in output.stdout.split("\n"):
    m = tagRE.match(line)
    if not m:
        continue
    m = m.groupdict()

    t = Tag()
    t.orig = line
    t.major = int(m["major"])
    t.minor = int(m["minor"])
    t.patch = int(m["patch"])
    t.rc = int(m["rc"] if m["rc"] is not None else 0)

    tags.append(t)

by_major_minor = {}

for tag in tags:
    key = (tag.major, tag.minor)
    l = by_major_minor.get(key, [])
    l.append(tag)
    by_major_minor[key] = l

latest_by_major_minor = []
for (mm, l) in by_major_minor.items():
    # sort ascending by patch-level (rc's weigh less)
    l.sort(key=lambda tag: (tag.patch, int(tag.rc == 0), tag.rc))
    latest_by_major_minor.append(l[-1])
latest_by_major_minor.sort(key=lambda tag: (tag.major, tag.minor))

cmdline = [
    "sphinx-multiversion",
    "-D", "smv_tag_whitelist=^({})$".format("|".join([re.escape(tag.orig) for tag in latest_by_major_minor])),
    "-D", "smv_branch_whitelist=^(master|stable)$",
    "-D", "smv_remote_whitelist=^.*$",
    "-D", "smv_latest_version=stable",
    "-D", r"smv_released_pattern=^refs/(tags|heads|remotes/[^/]+)/(?!master).*$", # treat everything except master as released, that way, the banner message makes sense
    # "--dump-metadata", # for debugging
    args.docsroot,
    args.outdir,
]
print(cmdline)
subprocess.run(cmdline, check=True)

# Unlink sphinxcontrib-versioning, sphinx-multiversion doesn't put the latest version at `/`.
# We don't want to break such links.
# The problem is that we're hosting on GitHub pages, so, we don't control the web server.
# So, time for a dirty hack: place an HTML-level redirect.
outdir = Path(args.outdir)
assert outdir.is_dir()
assert (outdir / "stable" / "index.html").is_file(), "sanity check"
def recurse(prefix: Path):
    srcdir = outdir / "stable" / prefix
    dstdir = outdir / prefix
    assert srcdir.is_dir(), f"{srcdir}"
    assert dstdir.is_dir(), f"{dstdir}"
    # print(outdir, prefix, srcdir, dstdir)
    for f in srcdir.glob("*"):
        assert not (dstdir / f.name).exists(), f"don't want to be overwriting stuff: {f}"
        if f.is_file():
            # redirect using JS, with fallback to `http-equiv`
            redirect_path = str(prefix / f.name)
            redirect = f"""
    <!DOCTYPE html>
    <meta charset="utf-8">
    <title>Redirecting...</title>
    <meta http-equiv="refresh" content="2; URL=/stable/{redirect_path}">
    <script>
        window.location.href = "/stable" + window.location.pathname + window.location.search + window.location.hash;
    </script>
    """
            (dstdir / f.name).write_text(redirect)
        elif f.is_dir():
            (dstdir / f.name).mkdir()
            recurse(prefix / f.name)
        else:
            assert False, "unsupported: {f}"
recurse(Path("."))
