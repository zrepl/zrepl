---
description: Update supporters list from git history and changelog contributors
argument-hint: <time-range-or-release>
model: sonnet
---

# Task

Automatically update `docs/supporters.rst` by extracting GitHub contributors from:
1. Git commit history (code contributors)
2. `docs/changelog.rst` (all mentioned GitHub users)

## Arguments

Specify a time range or release range to filter contributors:

**Time range examples:**
- `--since="2024-01-01"` - Contributors since a specific date
- `--since="1 year ago"` - Contributors in the last year
- `--since="6 months ago"` - Contributors in the last 6 months

**Release range examples:**
- `v0.6.0..HEAD` - Contributors between v0.6.0 and current HEAD
- `v0.6.0..v0.7.0` - Contributors between two releases
- `--all` - All contributors (default if no argument provided)

## Instructions

### Step 1: Extract Contributors from Git History

Parse the arguments to determine the git log range:
- If `--since="..."` is provided, use: `git log --since="..." --format='%aN <%aE>' | sort -u`
- If a commit range like `v0.6.0..HEAD` is provided, use: `git log v0.6.0..HEAD --format='%aN <%aE>' | sort -u`
- If `--all` or no argument, use: `git log --format='%aN <%aE>' | sort -u`

Parse the output to identify:
- GitHub users (look for @users.noreply.github.com emails or known GitHub patterns)
- Real names of code contributors
- Filter out the main maintainer (Christian Schwarz / problame) as they're already credited

### Step 2: Extract GitHub Users from Changelog

Read `docs/changelog.rst` and extract all GitHub usernames mentioned:
- Pattern: `` `@username <https://github.com/username>`_ ``
- Pattern: `@username` in text
- Collect all unique usernames

### Step 3: Categorize Contributors

For each contributor, determine their tier:

**Code contributors** (`|supporter-code|`):
- Anyone who appears in git history as a commit author
- Prioritize those with substantial contributions (multiple commits)

**Other contributors** (keep existing `|supporter-gold|` and `|supporter-std|`):
- Keep all existing gold/std supporters as-is (monetary supporters)
- Only update code contributors

### Step 4: Read Current Supporters File

Read `docs/supporters.rst` and parse existing entries to:
- Preserve all `|supporter-gold|` entries (monetary supporters - don't touch these)
- Preserve all `|supporter-std|` entries (monetary supporters - don't touch these)
- Preserve ALL existing `|supporter-code|` entries in their current order
- Extract list of existing code contributor names/GitHub usernames to avoid duplicates

### Step 5: Identify Contributors to Promote/Add

**IMPORTANT: Find contributors in the range, and either ADD them (if new) or MOVE them up (if recurring).**

For each GitHub user found in changelog or git history for the specified range:
1. Check if they're already listed as `|supporter-code|` in the existing file
2. Categorize them:
   - **NEW**: Not currently in the file → will be added
   - **RECURRING**: Already in the file with new contributions in this range → will be moved to top
   - **UNCHANGED**: Not in this range → stays in original position
3. For both NEW and RECURRING contributors:
   - Get their latest contribution date from the range: `git log <range> --author="email" --format="%ai" -1`
   - Format as: `* |supporter-code| `Name <https://github.com/username>`_`
   - Use their real name if available from git history, otherwise use GitHub username
4. Sort NEW + RECURRING contributors together by their contribution date (newest first)
5. Keep list of UNCHANGED contributors in their original order

**The final order will be: [NEW + RECURRING from range, sorted newest first] + [UNCHANGED in original order]**

### Step 6: Generate Updated File

Create the updated supporters list:
- Keep all `|supporter-gold|` entries unchanged and in their exact positions
- Keep all `|supporter-std|` entries unchanged and in their exact positions
- For `|supporter-code|` entries, reorganize as follows:
  1. **Top section**: Contributors from the analyzed range (NEW + RECURRING), sorted newest first
  2. **Bottom section**: UNCHANGED contributors (not in this range), in their original order
- Update the spacer comment showing the range that was just processed:
  ```rst
  ..
     ↓ claude --permission-mode default /update-supporters <actual-range-used>
  ```
  Replace `<actual-range-used>` with the actual argument passed (e.g., `v0.6.1..v0.7.0`)
  This comment should appear immediately before the first code contributor entry
  If there's an existing comment in this format, replace it; otherwise add it
- Maintain all other file structure and RST formatting, including the "Before /update-supporters" marker comment

### Step 7: Apply Changes

- Use Edit tool to update the `docs/supporters.rst` file
- Display summary of changes made:
  - Time/release range analyzed
  - Number of contributors found
  - New code contributors added
  - Final counts by tier

## Implementation Notes

- **DO NOT modify, remove, or re-sort** any `|supporter-gold|` or `|supporter-std|` entries
- **For code contributors**: ADD new ones and MOVE UP recurring ones (those with contributions in the analyzed range)
- When extracting from git history, focus on substantial contributors (filter out single-commit or trivial changes if needed)
- **CRITICAL:** Contributors from the analyzed range go to the top
  - Use `git log <range> --author="<email>" --format="%ai" -1` to get latest contribution date in the range
  - Both NEW and RECURRING contributors from this range are sorted together (newest first) and placed at the TOP
  - UNCHANGED contributors (not in this range) stay in their original relative order below
  - This creates a chronological history: most recent release's contributors appear first
- Add/update the spacer comment showing the processed range
- Handle edge cases:
  - Contributors mentioned in changelog but not in git history (external contributors, reporters)
  - Contributors in git history but not mentioned in changelog (decide whether to include)

## Example Output

```
Analyzing contributors for range: v0.6.0..v0.7.0
- Found 8 code contributors in git history
- Found 12 GitHub users mentioned in changelog.rst (v0.7.0 section)
- Current supporters.rst has 8 gold, 12 std, 10 code contributors

New contributors:
+ Jane Doe (@janedoe)
+ John Smith (@jsmith)

Promoted (recurring) contributors:
↑ Alice Developer (@alice) - new contributions in v0.6.0..v0.7.0

Updated docs/supporters.rst:
- Gold supporters: 8 (unchanged)
- Std supporters: 12 (unchanged)
- Code contributors: 10 → 12
- Promoted to top: 3 contributors (2 new + 1 recurring)
```
