---
description: Generate GitHub release notes from docs/changelog.rst and create a draft release
argument-hint: <version-tag, e.g. v0.7.0>
model: sonnet
allowed-tools: Read, Write
---

# Task

Create a draft GitHub release for zrepl version $ARGUMENTS.

## Inputs

Here is the current changelog RST source:

!`cat docs/changelog.rst`

## Instructions

1. Extract the changelog section for version $ARGUMENTS from the RST source above.
2. Extract all GitHub usernames mentioned in that changelog section (look for @username patterns or other contributor attributions).
3. Write a short "Highlights" section (2-4 bullets) summarizing the most impactful user-facing changes in plain language. Do NOT include commit/issue links.
4. Write a "Contributors" thank you line for all GitHub users found in step 2. Format as: "Thanks to @user1, @user2, and @user3 for their contributions to this release!"
5. Write a "Breaking Changes" section. Convert RST formatting to plain Markdown text (no commit or issue links):
   - `:commit:\`abc123\`` → just remove it or describe what it changed
   - `:issue:\`123\`` → just remove it or describe the issue in plain text
   - `:ref:\`display text <anchor>\`` → just use `display text` as plain text
   - `:repomasterlink:\`path\`` → just mention the path without a link
   - RST inline code ` ``code`` ` → markdown `` `code` ``
   - RST links `` `text <url>`_ `` → just use `text` without the link
   If there are no breaking changes, write "No breaking changes. vX.Y.Z-1 is interoperable with vX.Y.Z." (fill in actual versions).
6. Assemble the full release notes using the template below. **IMPORTANT**: Do NOT include a detailed changelog list. Only include what is specified in the template.
7. Write the result to `artifacts/release-notes.md`.
8. Validate all links in the release notes by checking HTTP status codes with curl:
   - Extract all URLs from the markdown file
   - Check each URL with `curl -sI -w "%{http_code}" -o /dev/null <url>`
   - Report any broken links (non-200 status codes)
   - If any links are broken, stop and notify the user before creating the release
9. Run: `gh release create $ARGUMENTS --title "$ARGUMENTS" --notes-file artifacts/release-notes.md --draft`
10. Print the release URL.

## Template

```
The full changelog is [on the docs site](https://zrepl.github.io/changelog.html).

## Highlights

{2-4 bullet plain-language summary of the most impactful changes}

{Thank all GitHub users mentioned in the changelog, e.g., "Thanks to @user1, @user2, and @user3 for their contributions to this release!"}

## Breaking Changes

{breaking changes, or "No breaking changes."}

## New Users

We provide [quick-start guides](https://zrepl.github.io/quickstart.html) for different usage scenarios.
We also recommend studying the [overview section of the configuration chapter](https://zrepl.github.io/configuration/overview.html).

## Testing & Upgrading

* Read the [Changelog](https://zrepl.github.io/changelog.html)
* [Run the platform tests](https://zrepl.github.io/usage.html#platform-tests) on a test system.
* Download & deploy the `zrepl` binary / distro package.

## Donations

zrepl is a spare-time project primarily developed by [Christian Schwarz](https://cschwarz.com).
Express your support through a donation to keep maintenance and feature development going.

[![Support me on Patreon](https://img.shields.io/badge/dynamic/json?color=yellow&label=Patreon&query=data.attributes.patron_count&suffix=%20patrons&url=https%3A%2F%2Fwww.patreon.com%2Fapi%2Fcampaigns%2F3095079)](https://patreon.com/zrepl) [![Donate via GitHub Sponsors](https://img.shields.io/static/v1?label=Sponsor&message=%E2%9D%A4&logo=GitHub&style=flat&color=yellow)](https://github.com/sponsors/problame) [![Donate via Liberapay](https://img.shields.io/liberapay/patrons/zrepl.svg?logo=liberapay)](https://liberapay.com/zrepl/donate) [![Donate via PayPal](https://img.shields.io/badge/donate-paypal-yellow.svg)](https://www.paypal.com/cgi-bin/webscr?cmd=_s-xclick&hosted_button_id=R5QSXJVYHGX96)
```
