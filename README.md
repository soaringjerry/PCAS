[简体中文](README.zh.md)

# PCAS (Personal Central AI System)

**PCAS is an open-source, local-first, intelligent decision-making engine designed to power a new generation of personal AI operating systems.**

It serves as the core technical heart of the **DreamHub Ecosystem**, a broader vision for a user-centric AI future built on the principle of "Absolute Data Sovereignty, Flexible Compute Scheduling."

---

## 📖 What is PCAS?

PCAS is not a user-facing application. It is a **deployable software engine** that you run in your private environment (e.g., your PC or home server). Its sole purpose is to act as a secure and intelligent "decision-making center" for your digital life.

It connects to various applications and services (we call them D-Apps) through an **Intelligent Event Bus**, allowing you to create powerful, automated workflows while ensuring your data never leaves your control.

> To delve deeper into the philosophy and technicals, please read the **[PCAS Whitepaper](docs/WHITEPAPER.md)** and the **[PCAS Technical Plan](docs/PCAS_PLAN.md)**.

## ✨ Core Features

*   **🛡️ Absolute Data Sovereignty:** PCAS and your data run in your private environment. You have full control. Period.
*   **🎛️ Flexible Compute Modes:** Through a built-in "Policy Engine," you decide how AI tasks are processed:
    *   **Local Mode:** Maximum privacy with local AI models.
    *   **Hybrid Mode:** The perfect balance of privacy and performance.
    *   **Cloud Mode:** Maximum power using cloud AI APIs.
*   **🤖 Intelligent Decision-Making:** PCAS acts as your "Personal Decision Center," understanding your intent and coordinating D-Apps to get things done.
*   **🧩 Open D-App Ecosystem:** The event bus architecture allows any service to be integrated as a D-App.
*   **🚀 Foundation for Personal AI:** PCAS is designed to be a "Data Crucible," helping you build a private dataset to fine-tune your own personal AI models.
*   **🌐 Open Standard & Community:** We aim for PCAS to become an open standard for a new pattern of personal AI.

## 🏛️ Architecture

PCAS is the central hub in a mesh-like, event-driven network of D-Apps.

```mermaid
graph TD
    subgraph "The World"
        DApp1[D-App: Communicator]
        DApp2[D-App: Scheduler]
        DAppN[More D-Apps...]
    end

    subgraph "Your Private Environment"
        PCAS_Core[PCAS Engine]
    end

    %% Communication Flow
    DApp1 <--> |Events/Commands via Secure Bus| PCAS_Core
    DApp2 <--> |Events/Commands via Secure Bus| PCAS_Core
    DAppN <--> |Events/Commands via Secure Bus| PCAS_Core

    style PCAS_Core fill:#cde4ff,stroke:#36c,stroke-width:3px
```

## 🚀 Quick Start

This guide will walk you through experiencing PCAS's core "semantic memory" capability - from storing a memory to performing semantic search.

### 1. Prerequisites

Before you begin, ensure you have:
- An OpenAI API key
- A running ChromaDB instance (default: `http://localhost:8000`)

### 2. Configuration

PCAS behavior is driven by the `policy.yaml` file. Here's a minimal configuration to get started:

```yaml
version: v1
providers:
  - name: mock-provider
    type: mock
  - name: openai-gpt4
    type: openai

rules:
  - name: "Rule for PCAS memory events"
    if:
      event_type: "pcas.memory.create.v1"
    then:
      provider: mock-provider
```

### 3. Build and Run

Build the project:
```bash
make build
```

In a new terminal, start the PCAS service:
```bash
export OPENAI_API_KEY="your-api-key-here"
export CHROMA_URL="http://localhost:8000"
./bin/pcas serve
```

### 4. Interact with PCAS

**Store a memory:**
```bash
./bin/pcasctl emit --type pcas.memory.create.v1 \
  --subject "The project's core principle is 'Absolute Data Sovereignty, Flexible Compute Scheduling.'"
```

**Search for memories:**
```bash
./bin/pcasctl search "What is the foundational philosophy of the project?"
```

You should see the original memory returned in the search results, demonstrating PCAS's semantic understanding capability.

## 🤝 Community & Contribution

PCAS is an open-source project driven by the community. We sincerely invite you to join us.

*   **Join the discussion:** [Discord Link TBD]
*   **Contribute:** Please read our `CONTRIBUTING.md` (TBD).
*   **Report issues:** Please use the Issues section.

## 📄 License

PCAS is open-sourced under the [MIT License](LICENSE).