# ADR 003: D-App Ecosystem Collaboration Model

**Date**: 2025-07-06

**Status**: Proposed

**Context**:

As the DreamHub ecosystem grows beyond a single application, we face a fundamental architectural challenge: how to enable complex, multi-dApp workflows (e.g., a "Notes" dApp consuming data from a "Transcription" dApp) without creating tight coupling between them. Direct dependencies would violate the "dApp as an independent island" principle and break the causality chainintegrity of the PCAS event graph. We need a model that supports inter-dApp data flow while maintaining strict decoupling and central orchestration through PCAS.

**Decision**:

We will adopt the "**Tiered Provider Network**" model (also conceptualized as a "Personal Internet") as the foundational architecture for the DreamHub dApp ecosystem. This model is implemented through an **Event-Driven Choreography** pattern.

This model is defined by the following core principles:

1.  **D-Apps as Independent Responders**: Every dApp (e.g., DreamTrans, DreamNote) is an autonomous "island" or "Tier 2/3 provider". They do not know about each other. They operate by subscribing to events they are interested in and publishing events that represent their output or intent.

2.  **PCAS as the Event Bus (The Ocean/Backbone)**: PCAS is the sole "Tier 1 provider". It acts as the central, reliable event bus, but it does not orchestrate or command the d-Apps. It only facilitates communication.

3.  **Collaboration via Event Choreography**: The collaboration between d-Apps is achieved asynchronously and indirectly by publishing and subscribing to well-defined, generic "intent events".
    *   **Example**: To start a recording session, DreamNote does not call DreamTrans. Instead, it publishes a generic `ecosystem.session.recording.request.v1` event. DreamTrans, having subscribed to this event type, receives it and independently decides to start its recording process. This achieves collaboration without coupling.

4.  **Service D-Apps for Metadata Enrichment**: A special class of d-Apps, "Service d-Apps" (e.g., `GeoTagger-dApp`), can run in the background. They subscribe to broad event categories and enrich the ecosystem by publishing new "metadata events" (e.g., `pcas.metadata.location.added.v1`) that are linked to the original events, thus progressively making the entire memory graph smarter.

4.  **Capability Contracts (`dapp.yaml`)**: To make this model explicit and manageable, each dApp MUST include a `dapp.yaml` manifest. This file declares:
    *   `provides`: A list of event types the dApp can emit, representing the capabilities it offers to the ecosystem.
    *   `requires`: A list of capabilities (event types) that must be present in the ecosystem for this dApp to function correctly.

5.  **Real-time Interaction via `InteractStream` RPC**: The event choreography model is ideal for asynchronous, decoupled workflows. However, it is not suitable for low-latency, real-time interactions like live translation or conversational AI. To address this, we introduce a dedicated bi-directional streaming RPC, `InteractStream`, on the `EventBusService`.
    *   **Purpose**: This RPC provides a "hotline" for d-Apps that require immediate, back-and-forth communication with a backend AI provider, bypassing the standard asynchronous event publishing model.
    *   **Decoupling**: Crucially, decoupling is still maintained by the PCAS Policy Engine. The dApp initiates the stream by specifying an `event_type` (e.g., `dapp.aipen.translate.stream.v1`). The Policy Engine uses this `event_type` to route the entire stream to the appropriate, stream-capable provider (e.g., `openai-streaming-provider`), without the dApp needing to know which provider is handling the request.
    *   **Semantic Scoping**: The responsibility for "semantic slicing" (e.g., determining when a sentence is complete) lies with the dApp, as it is closest to the user's context. The dApp sends complete semantic units, and PCAS handles any necessary technical slicing to fit the backend model's constraints.

6.  **UI Integration via a Launcher**: To provide a seamless user experience, a **DreamHub Launcher** application will be responsible for:
    *   Reading the `dapp.yaml` manifests of all installed d-Apps.
    *   Dynamically constructing a "Composite UI" for specific user scenarios (e.g., "Start a Smart Meeting") by combining the UI components of the required d-Apps.

**Consequences**:

*   **Benefits**:
    *   **Maintains Decoupling**: This model strictly enforces the "dApp as an island" principle, preventing a "spaghetti" architecture.
    *   **Preserves Causality**: Every significant step (session completion, summary request, summary creation) is a distinct event, ensuring the integrity of the PCAS data graph for future auditing and learning.
    *   **Enhances PCAS's Value**: It solidifies PCAS's role as the indispensable intelligent orchestrator, not just a passive event bus.
    *   **Fosters a Healthy Ecosystem**: Any third-party developer can create a dApp that either `provides` a new capability or `requires` an existing one, allowing for permissionless innovation.

*   **Drawbacks/Costs**:
    *   **Increased Indirection**: The logic flow is less direct than a simple API call, which may require a steeper learning curve for new developers.
    *   **Launcher Complexity**: The DreamHub Launcher becomes a critical and potentially complex piece of infrastructure.
    *   **Strict Governance Required**: This model relies on strict adherence to event schemas and the `dapp.yaml` contract.

### System Resilience and Fault Tolerance

To ensure the robustness of this distributed system, the following principles for error handling and fault tolerance are considered an integral part of this ADR:

1.  **Standardized Error Events**: When a dApp or provider fails to process an event, it MUST publish a standardized `pcas.error.v1` event. This error event MUST contain the ID of the original event that caused the failure.

2.  **Retry Policies**: D-Apps and Providers SHOULD implement their own idempotent retry logic (e.g., exponential backoff) for transient failures.

3.  **Dead Letter Queue (DLQ)**: PCAS Core will provide a Dead Letter Queue mechanism. If an event consistently fails to be processed by any subscriber after a certain number of retries, it will be moved to the DLQ for later inspection and manual intervention. This prevents "poison pill" events from halting the entire system.

4.  **Health Checks**: D-Apps SHOULD expose a simple health check endpoint that the Launcher or other system tools can use to monitor their status.

This ADR establishes the definitive architectural pattern for all future dApp development within the DreamHub ecosystem.