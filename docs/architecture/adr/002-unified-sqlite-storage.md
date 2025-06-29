---
title: "ADR 002: Unified, Zero-Dependency SQLite Storage Backend"
description: "Architectural Decision Record for implementing a zero-dependency, pure Go SQLite-based default storage backend for PCAS, including integrated vector search capabilities."
tags: ["adr", "architecture", "storage", "sqlite", "go"]
status: "Approved"
date: "2025-06-28"
version: "0.1.2"
---

# ADR 002: Unified, Zero-Dependency SQLite Storage Backend (v4 - Final Locked)

**Status**: Approved
**Date**: 2025-06-28
**Decision Makers**: Roo (Architect), o3 (Planner)

## 1. Context

为了解决 PCAS 对 Docker 的依赖问题，并统一所有平台的用户体验，我们决定实现一个零依赖的、基于纯 Go SQLite 的默认存储后端。本 ADR v4 版本在采纳了所有评审意见后，形成了最终的工程实施方案。

## 2. 决策

我们将实现一个以 `modernc.org/sqlite` 为核心、内置纯 Go 向量搜索能力的、可插拔的 `StorageProvider`，并将其作为 PCAS 的默认存储方案。

- **核心技术栈**:
  - **数据库**: `modernc.org/sqlite` (纯 Go, CGO-free)。
  - **向量能力**: 集成一个**纯 Go、无 CGO 依赖的 HNSW (Hierarchical Navigable Small World) 算法库**。

- **“双模式”产品策略**:
  - **默认模式**: 所有用户下载的应用，将默认使用此 SQLite + HNSW 后端，实现解压即用。
  - **高级模式**: 保留原有的 `PostgreSQL` 后端，作为面向专家用户的可选模式。

## 3. 风险评估与缓解策略

| 风险点 | 可能影响 | 缓解思路 |
| :--- | :--- | :--- |
| **纯 Go HNSW 库选型** | 生态分散，质量参差不齐，并发和持久化支持不一 | 1. **首要标准**: 必须是纯 Go、无 CGO 依赖。2. **PoC 选型**: 对候选库进行严格的基准测试。3. **接口隔离**: 内部对向量库的调用再做一层封装，方便未来热插拔替换。 |
| ... | ... | ... |

## 4. 实施计划与版本策略

我们将采用迭代式开发和语义化版本控制，来稳步推进此项重构。

### **第一阶段：v0.1.1 - 基础能力 MVP**
- **核心任务**: 完成“实施计划”中的**第一步**和**第二步**。
  - **(1)** 在现有 `modernc/sqlite` 基础上，增加向量存储能力。
  - **(2)** 实现一个基于“暴力搜索”的、功能可用的 `QuerySimilar` 接口。
- **交付成果**: 一个功能完整的、零依赖的 PCAS 核心。

### **第二阶段：v0.1.x - 优化与迭代**
- **核心任务**: 根据用户反馈，对 `v0.1.1` 版本进行 bug 修复和体验优化。

### **第三阶段：v0.2.0 - 高性能版本**
- **核心任务**: 完成“实施计划”中的**第三步**。
  - **(3)** 将 PoC 中选定的最优 HNSW 库，集成到 SQLite 后端中，替代暴力搜索。
- **交付成果**: 一个性能强大的、可满足绝大多数用户长期需求的 PCAS 正式版。

## 5. PoC 前置任务
在所有开发开始前，必须先完成**“第零步：PoC 与基准测试”**，以验证 HNSW 库的技术选型。

...