# Idea: PCAS Core Scalability Model

**Date**: 2025-07-06

**Status**: Idea

**Context**:
As the number of d-Apps and the volume of events increase, the single-node PCAS instance could become a performance bottleneck.

**Proposal**:
- Introduce an event `priority` field in the event envelope to allow PCAS to process high-priority tasks first.
- Evolve the Policy Engine to support dynamic, load-based routing.
- Explore a future "cluster mode" for PCAS to enable horizontal scaling.