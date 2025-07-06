# DreamTrans & PCAS Collaboration Guide

**Version**: 0.1
**Status**: Draft
**Audience**: Developers of DreamTrans (and similar dApps)

## 1. Goal of this Guide

This guide aims to provide a clear, actionable integration plan for DreamTrans, ensuring it can leverage the core capabilities of PCAS to the fullest and become a powerful, reliable, and decoupled service within the PCAS ecosystem.

This document serves as the "architectural blueprint" for the collaboration between DreamTrans and PCAS.

## 2. Core Philosophy: Two Developer Roles, Two Sets of Responsibilities

To understand the PCAS collaboration model, one must first distinguish between two distinct developer roles:

| Role | dApp Developer (e.g., author of DreamTrans) | PCAS Provider Developer (e.g., PCAS core team) |
| :--- | :--- | :--- |
| **Worldview** | "I have an **application** that needs an AI **capability**." | "I have an AI **capability** that needs to be encapsulated as a **service**." |
| **Work** | Develops UIs, handles user interactions, translates user intent into PCAS events. | Develops specific Providers, encapsulating API integration, authentication, error handling, and **Prompt construction** for a specific AI model (e.g., OpenAI, Llama 3). |
| **Language** | Any language that can call gRPC (Go, Python, TypeScript, ...). | Go, as the PCAS core is written in Go. |
| **Touches Prompts?** | **No! Absolutely not!** | **Yes!** This is one of their core responsibilities. |

**Conclusion: To build a dApp, you do not need to hard-code any Prompts in `.go` files.** A dApp developer only needs to send an event with a clear `event.type` and the required `attributes`, according to the agreed-upon contract.

## 3. Prompt Construction: A Flexible, Configurable Template Mechanism

To strike a balance between "ease of use" and "high flexibility," PCAS employs a two-tier, configurable Prompt construction mechanism. **Users and dApp developers can define Prompts in `policy.yaml`.**

### 3.1 Two-Tier Template Priority

When a Provider needs to construct a Prompt, it looks for a template in the following order:

1.  **User-Defined Template (Highest Priority)**: A `prompt_template` defined directly within the `then` block of a rule in the user's `policy.yaml`. This gives the user the final, ultimate control.
2.  **Provider Default Template (Lowest Priority)**: A template hard-coded in the Provider's Go code, serving as an out-of-the-box fallback.

### 3.2 The Collaborative Workflow

1.  **dApp (DreamTrans)**:
    *   A user types "Hello".
    *   DreamTrans initiates a stream request with `event_type: "dapp.dreamtrans.translate.stream.v1"` and sets `attributes: {"target_language": "en"}`.
    *   It sends "Hello" as `StreamData`.
    *   **Its job is done.** It has no knowledge of the Prompt.

2.  **PCAS (Policy Engine)**:
    *   Receives the `event_type`, looks up `policy.yaml`.
    *   Finds a matching rule that points to `openai-provider` and **may** contain a `prompt_template`.
    *   Passes the stream, `attributes`, and the optional `prompt_template` to the `openai-provider`.

3.  **Provider (`openai-provider`)**:
    *   Checks if it received a `prompt_template`.
    *   **If yes**, it uses that template, combining it with dynamic data from `attributes` and `StreamData` to construct the final Prompt.
    *   **If no**, it uses its own hard-coded default template.
    *   Calls the OpenAI API and returns the result.

This model ensures the **simplicity** of dApp development and the **flexibility** of the PCAS system.

## 4. Key Responsibility: Semantic Slicing

*   **Caller Responsibility**: The client (dApp) calling the `InteractStream` RPC **MUST** be responsible for segmenting the user's continuous input into meaningful semantic units (e.g., complete sentences or questions).
*   **PCAS's Role**: PCAS **will NOT** perform sentence segmentation or semantic slicing on streaming data. It treats every `StreamData` message it receives as an independent, complete unit for processing.