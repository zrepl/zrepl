import argparse
from pathlib import Path
import re

import requests

import time

import os
import yaml
import argparse
from pathlib import Path


circle_token = os.environ.get('CIRCLE_TOKEN')
if not circle_token:
    cli_yml = Path.home() / ".circleci" / "cli.yml"
    if cli_yml.exists():
        with open(cli_yml) as f:
            data = yaml.safe_load(f)
            if data:
                circle_token = data.get("token")
if not circle_token:
    raise ValueError('CIRCLE_TOKEN not set and no token found in ~/.circleci/cli.yml')

parser = argparse.ArgumentParser(description='Download artifacts from CircleCI')
parser.add_argument('build_num', type=str, help='Build number')
parser.add_argument('dst', type=Path, help='Destination directory')
parser.add_argument('--prefix', type=str, default='', help='Filter for prefix')
parser.add_argument('--match', type=str, default='.*', help='Only include paths matching the given regex')

args = parser.parse_args()

res = requests.get(
    f"https://circleci.com/api/v1.1/project/github/zrepl/zrepl/{args.build_num}/artifacts",
    headers={
        "Circle-Token": circle_token,
    },
)
res.raise_for_status()

# https://circleci.com/docs/api/v1/index.html#artifacts-of-a-job
# [ {
#   "path" : "raw-test-output/go-test-report.xml",
#   "pretty_path" : "raw-test-output/go-test-report.xml",
#   "node_index" : 0,
#   "url" : "https://24-88881093-gh.circle-artifacts.com/0/raw-test-output/go-test-report.xml"
# }, {
#   "path" : "raw-test-output/go-test.out",
#   "pretty_path" : "raw-test-output/go-test.out",
#   "node_index" : 0,
#   "url" : "https://24-88881093-gh.circle-artifacts.com/0/raw-test-output/go-test.out"
# } ]
res = res.json()

for artifact in res:
    if not artifact["pretty_path"].startswith(args.prefix):
        continue
    if not re.match(args.match, artifact["pretty_path"]):
        continue
    stripped = artifact["pretty_path"][len(args.prefix):]
    print(f"Downloading {artifact['pretty_path']} to {args.dst / stripped}")
    artifact_rel = Path(stripped)
    artifact_dst = args.dst / artifact_rel
    artifact_dst.parent.mkdir(parents=True, exist_ok=True)

    res = requests.get(
        artifact["url"],
        headers={
            "Circle-Token": circle_token,
        },
        stream=True,
    )
    res.raise_for_status()

    total_size = int(res.headers.get("Content-Length", 0))
    block_size = 128 * 1024
    with open(artifact_dst, "wb") as f:
        progress = 0
        start_time = time.time()
        for chunk in res.iter_content(chunk_size=block_size):
            f.write(chunk)
            progress += len(chunk)
            percent = progress / total_size * 100
            elapsed_time = time.time() - start_time
            if elapsed_time >= 5:
                print(f"Downloaded {progress}/{total_size} bytes ({percent:.2f}%)", end="\r")
                start_time = time.time()
        print(f"Downloaded {progress}/{total_size} bytes ({percent:.2f}%)")
    print("Download complete!")

print("All files downloaded")
