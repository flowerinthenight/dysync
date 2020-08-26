![Main](https://github.com/flowerinthenight/dysync/workflows/Main/badge.svg)

## Overview

`dysync` is a tool for syncing [DynamoDB](https://aws.amazon.com/dynamodb/) tables between two accounts.

When role ARN is provided (either by `--src-rolearn` or `--dst-rolearn`, this tool will attempt to assume that role using the provided key/secret combination.

By default, this tool will attempt to do a full sync, meaning, any records in dst that don't exist in src will be deleted. To override, set `--copy-only` to true, in which case, it will only do a copy. This option only works when both `--id` and `--sk` are empty (scan table), otherwise, if either `--id` or `--sk` is set (or both), it will do a copy only, not full sync.

At the moment, this tool only supports string-based primary keys and sort keys (when using `--id` and/or `--sk` flags).

Note that this tool uses the Scan AWS API which is expensive and really slow for huge tables. You've been warned.

## Installation

Using [Homebrew](https://brew.sh/) (applies to Linux, OSX, and [WSL](https://en.wikipedia.org/wiki/Windows_Subsystem_for_Linux)):
```bash
$ brew tap flowerinthenight/tap
$ brew install dysync
```

If you have a Go environment:
```bash
$ go get -u -v github.com/flowerinthenight/dysync
```

## Usage
```bash
$ dysync --src-key xxx --src-secret xxx --dst-key xxx --dst-secret xxx [--dryrun] TABLE_NAME

# For a more updated help information:
$ dysync -h
```

## Need help
PR's are welcome!

- [ ] Support for non-string primary keys and/or sort keys.
