# BanyanDB Command Line Tool

`bydbctl` is a command line tool to interact with BanyanD.

## BYDBQL Agent TUI

`bydbctl agent` uses any compatible ACP provider to discover schemas and submit typed query plans in a three-page terminal workspace. bydbctl owns the
controlled tool bridge, deterministically renders BYDBQL, and requires one-time approval before every query execution. See the
[BYDBQL Agent TUI guide](../docs/interacting/bydbctl/agent.md).

## Build

```
# If you haven't generated the API code yet
make -C ../api generate

make build
```
