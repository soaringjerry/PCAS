---
name: "Tech Debt: Resolve golangci-lint typecheck errors"
about: Resolve golangci-lint typecheck errors in vendor mode
title: "chore(lint): Resolve golangci-lint typecheck errors in vendor mode"
labels: tech-debt, linting, P2-normal
assignees: ''
---

## Description

`golangci-lint` reports `typecheck` related errors when running in vendor mode, which appear to be false positives. We need to investigate golangci-lint configuration and adjust settings to eliminate these false positives without lowering our actual linting standards.

## Current State

- golangci-lint reports undefined symbols for vendored packages
- CI has `continue-on-error: true` for the lint step
- go vet passes without issues
- Code compiles successfully with `go build -mod=vendor`

## Acceptance Criteria

- [ ] Remove `continue-on-error: true` from the `make lint` step in ci.yml
- [ ] CI remains green after removal
- [ ] All legitimate linting issues are still caught
- [ ] No false positives for vendored dependencies

## Technical Details

- Errors appear to be related to yaml.v3 and openai-go packages
- May need to configure golangci-lint to properly handle vendor mode
- Consider creating .golangci.yml with appropriate settings
- Alternative: investigate if we should move away from vendor mode