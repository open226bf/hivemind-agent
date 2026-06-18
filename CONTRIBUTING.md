# Contributing to hivemind-agent

`hivemind-agent` is the dial-out reverse-tunnel agent for
[Hivemind](https://github.com/open226bf/hivemind). Shared contribution
conventions, Code of Conduct, and security reporting live in the control-plane
repo:

- **Contributing guide:** <https://github.com/open226bf/hivemind/blob/main/CONTRIBUTING.md>
- **Code of Conduct:** <https://github.com/open226bf/hivemind/blob/main/CODE_OF_CONDUCT.md>
- **Security policy:** <https://github.com/open226bf/hivemind/blob/main/SECURITY.md>

## Local development

```bash
go build ./...
go test ./...
```

The agent dials out to a Hivemind control plane; see
[Enroll an agent](https://open226bf.github.io/hivemind-doc/guides/enroll-an-agent/)
and the design notes in `docs/agent-design.md` (control-plane repo).

## Before opening a PR

```bash
gofmt -l .        # must be empty
go vet ./...
go build ./...
go test -race ./...
```

CI runs all of the above on every pull request.
