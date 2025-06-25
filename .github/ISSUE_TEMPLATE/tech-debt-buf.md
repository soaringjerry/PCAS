---
name: "Tech Debt: Fix buf version issues"
about: Fix buf version or configuration issues in CI
title: "chore(ci): Fix buf version or configuration issues in CI"
labels: tech-debt, ci-cd, P2-normal
assignees: ''
---

## Description

Currently `make proto` may produce warnings in local and CI environments due to `buf` version mismatches. We need to standardize the `buf` version and ensure that both `make proto` and `buf breaking` commands run stably without warnings in CI.

## Current State

- buf.yaml and buf.gen.yaml are using v1 format
- CI has `continue-on-error: true` for proto generation steps
- Local development may require manual buf installation

## Acceptance Criteria

- [ ] Remove `continue-on-error: true` from the `make proto` step in ci.yml
- [ ] Remove `continue-on-error: true` from the `buf breaking` step in ci.yml
- [ ] CI remains green after removal
- [ ] Document the required buf version in README or development guide

## Technical Details

- Consider pinning buf version in CI
- May need to update buf configuration files to match CI environment
- Ensure vendor mode compatibility