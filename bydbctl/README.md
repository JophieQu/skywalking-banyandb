# BanyanDB Command Line Tool

`bydbctl` is a command line tool to interact with BanyanD.

## BYDBQL Agent TUI

`bydbctl agent` uses an ACP agent to draft and revise BYDBQL in a three-page terminal workspace. bydbctl owns the controlled tool bridge and requires
one-time approval before every query execution. See the [BYDBQL Agent TUI guide](../docs/interacting/bydbctl/agent.md).

## Build

```
# If you haven't generated the API code yet
make -C ../api generate

make build
```
