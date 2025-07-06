# Idea: Distributed Observability Strategy

**Date**: 2025-07-06

**Status**: Idea

**Context**:
As our dApp ecosystem grows, debugging and monitoring a distributed, event-driven choreography system will become increasingly complex.

**Proposal**:
- Formally adopt **OpenTelemetry** as the standard for distributed tracing.
- Enforce strict propagation of `trace_id` across all event hops.
- Plan for a built-in, developer-friendly event flow visualization and tracing tool within a future "Developer Portal".