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
    "-D", "smv_branch_whitelist=^master$",
    "-D", "smv_remote_whitelist=^.*$",
    "-D", "smv_latest_version=master",
    "-D", r"smv_released_pattern=^refs/(tags|heads|remotes/[^/]+)/(?!master).*$", # treat everything except master as released, that way, the banner message makes sense
    # "--dump-metadata", # for debugging
    args.docsroot,
    args.outdir,
]
print(cmdline)
subprocess.run(cmdline, check=True)
