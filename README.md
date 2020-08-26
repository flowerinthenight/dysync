![Main](https://github.com/flowerinthenight/dysync/workflows/Main/badge.svg)

## Overview

`dysync` is a tool for syncing [DynamoDB](https://aws.amazon.com/dynamodb/) tables between two accounts.

When role ARN is provided (either by `--src-rolearn` or `--dst-rolearn`, this tool will attempt to assume that role using the provided key/secret combination.

By default, this tool will attempt to do a full sync, meaning, any records in dst that don't exist in src will be deleted. To override, set `--copy-only` to true, in which case, it will only do a copy. This option only works when both `--id` and `--sk` are empty (scan table), otherwise, if either `--id` or `--sk` is set (or both), it will do a copy only, not full sync.

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

To authenticate to AWS, this tool looks for the following environment variables (can be set by cmdline args as well):
```bash
# If --region, --key, --secret, and optionally, --rolearn are not provided, the tool
# will look for these environment variables:
AWS_REGION
AWS_ACCESS_KEY_ID
AWS_SECRET_ACCESS_KEY

# Optional:
ROLE_ARN

# Authenticate using id/secret flags:
$ lsdy --region=xxx --key=xxx --secret=xxx

# Authenticate by assuming a role ARN, in which case, id/secret should have the
# AssumeRole permissions. Using flags:
$ lsdy --region=xxx --key=xxx --secret=xxx --rolearn=xxx
```

To query a table using a primary key:
```bash
# Query table with primary key 'id' value of 'ID0001':
$ lsdy TABLE_NAME --pk "id:ID0001"
```

To query a table using both a primary key and a sort key:
```bash
# Query table with primary key 'id' value of 'ID0001' and sort key 'sortkey' of SK002:
$ lsdy TABLE_NAME --pk "id:ID0001" --sk "sortkey:SK002"
```

To query a table using multiple primary keys and optional sort key pair(s):
```bash
# Multiple primary keys only:
$ lsdy TABLE_NAME --pk "id:ID0001" --pk "id:ID0002" --pk "id:ID9999"

# Multiple primary keys with corresponding sort keys:
$ lsdy TABLE_NAME --pk "id:ID0001,id:ID0002,id:ID9999" --sk "sortkey:AAA,sortkey:BBB,sortkey:CCC"

# Multiple primary keys with only the first pk having a sortkey pair:
$ lsdy TABLE_NAME --pk "id:ID0001,id:ID0002,id:ID9999" --sk "sortkey:AAA"
```

To scan a table:
```bash
# All attributes (columns) will be queried:
$ lsdy TABLE_NAME

# If you want specific attributes (unsorted columns):
$ lsdy TABLE_NAME --attr "col1,col2,col3" --nosort

# or you can write it this way (sorted columns):
$ lsdy TABLE_NAME --attr col1 --attr col2 --attr col3
```

If you want to describe a table:
```bash
# Will output the table details and all its attributes/columns:
$ lsdy TABLE_NAME --describe
```
_Warning!_ At the moment, `--describe` will cause a table scan if the `--pk` flag is not set. For massive tables, it's probably a good idea to supply the `--pk` flag, in which case, it will only query the attributes from that key.

By default, the maximum length of all cell items in the output table is set by the `--maxlen` flag.

## Need help
PR's are welcome!

- [x] Handling data tabulation for fullwidth characters (i.e. Japanese, Chinese, etc.) - use [tablewriter](https://github.com/olekukonko/tablewriter)
- [x] Filtering/exclusion support - added with the `--contains` flag
- [ ] Better handling of JSON, map values in cells
- [x] Better handling of base64-encoded values in cells - added with the `--decb64` flag
- [ ] Query secondary indeces
- [ ] Support for other sort key types
- [ ] Config file support
- [x] ~~Package for Windows~~ - can use WSL for now
- [x] Output to CSV - added with the `--csv` flag
- [x] Add `--delete` option to delete the queried data
- [x] Add `--limit` option in query (Scan and Query)
