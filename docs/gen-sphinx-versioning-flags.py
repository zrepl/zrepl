#!/usr/bin/env python3

import subprocess
import re

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

# print(by_major_minor)
# print(latest_by_major_minor)

cmdline = []

for latest_patch in latest_by_major_minor:
    cmdline.append("--whitelist-tags")
    cmdline.append(f"^{re.escape(latest_patch.orig)}$")

# we want to render the latest non-rc version as the default page
# (latest_by_major_minor is already sorted)
default_version = latest_by_major_minor[-1]
for tag in reversed(latest_by_major_minor):
    if tag.rc == 0:
        default_version = tag
        break

cmdline.extend(["--root-ref", f"{default_version}"])
cmdline.extend(["--banner-main-ref", f"{default_version}"])
cmdline.extend(["--show-banner"])
cmdline.extend(["--sort", "semver"])

cmdline.extend(["--whitelist-branches", "master"])

print(" ".join(cmdline))