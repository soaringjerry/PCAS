---
title: "开发日志：部署流程、CI/CD 与客户端工具链强化"
date: 2025-08-03
tags: ["dev-log", "deployment", "ci-cd", "docker", "architecture"]
version: "v0.1.2"
author: "Roo"
---

# 开发日志：部署流程、CI/CD 与客户端工具链强化

本文档旨在详细记录 PCAS 项目在 v0.1.2 版本周期内，围绕**服务部署、CI/CD 自动化、客户端工具链健壮性**等方面取得的一系列关键进展、架构决策与问题修复。

## 核心成果总结

经过一系列深入的诊断与修复，我们成功地将 PCAS 的部署与发布流程提升到了一个全新的、生产级别的标准。核心成果包括：

1.  **标准化的生产部署流程**：确立了一套基于 Docker 的、包含数据持久化和外部化配置的最佳实践部署方案。
2.  **健壮的 Docker 镜像**：修复了 `Dockerfile` 中的数据卷权限问题，确保服务在非 root 安全模式下能够稳定运行。
3.  **全自动的发布流水线**：建立了由 Git 标签触发的 GitHub Actions 工作流，实现了 Docker 镜像的自动化构建、多标签生成 (`latest`, `版本号` 等) 并发布到 GitHub Container Registry (GHCR)。
4.  **可靠的客户端工具**：诊断并明确了 `pcasctl` 客户端在处理 gRPC 连接参数时的 Bug，并提供了可靠的临时解决方案。
5.  **清晰的架构原则**：通过讨论，进一步明确并记录了多个关键架构决策，如 Docker 对于 Go 应用的价值、D-App 生态中能力提供方与消费方的职责划分等。

---

## 最终的标准部署手册

这是未来部署或更新 PCAS 服务的权威指南，整合了我们所有的最佳实践。

### 前提

*   服务器上已安装 Docker。
*   您已经在服务器上准备好了您的配置文件，例如 `~/pcas_config/policy.yaml`。
*   您已经编译好了 `pcasctl` 客户端工具用于测试。

### 操作步骤

**第 1 步：拉取最新的稳定镜像**

```bash
docker pull ghcr.io/soaringjerry/pcas:latest
```

**第 2 步：清理旧的容器实例 (如果是更新操作)**

```bash
docker stop pcas-instance || true
docker rm pcas-instance || true
```

**第 3 步：确保数据卷存在 (只需执行一次)**

```bash
docker volume create pcas_data
```

**第 4 步：启动 PCAS 服务**

```bash
docker run -d --name pcas-instance \
  -p 50051:50051 \
  -e OPENAI_API_KEY="sk-your-key-here" \
  -v ~/pcas_config/policy.yaml:/app/policy.yaml:ro \
  -v pcas_data:/data \
  ghcr.io/soaringjerry/pcas:latest \
  /app/bin/pcas serve --db-path /data/pcas.db
```

---

## 关键问题诊断与修复回顾

### 1. `Dockerfile` 数据卷权限问题

*   **问题**: 容器以非 root 用户 `pcas` 运行，但挂载的 `/data` 目录归属于 `root`，导致 `pcas` 用户无法写入数据库文件，服务启动失败。
*   **诊断**: 通过 `docker logs` 发现 SQLite 报出误导性的 `out of memory` 错误，结合对 `Dockerfile` 的分析，最终定位为权限问题。
*   **修复**: 在 `Dockerfile` 中，于 `USER pcas` 指令前，增加了 `RUN mkdir /data && chown pcas:pcas /data`，在镜像构建阶段就预先创建目录并设定好正确的权限。

### 2. CI/CD 的 `:latest` 标签未更新问题

*   **问题**: 修复 `Dockerfile` 并推送代码后，`docker pull ...:latest` 拉取到的仍然是旧的、有问题的镜像。
*   **诊断**: 通过指导用户检查 GitHub Actions 日志，发现 `docker/build-push-action` 的输出中只包含了基于 commit SHA 的标签，没有 `:latest` 或版本号标签。最终定位问题为 `docker/metadata-action` 配置不完整。
*   **修复**: 为 Claude 准备了任务卡，指导其为 `metadata-action` 添加了 `type=raw,value=latest` 和 `type=semver,pattern={{version}}` 等规则，使其能够从 Git 标签中正确地生成多种 Docker 标签。

### 3. `pcasctl` 客户端连接失败问题

*   **问题**: `pcasctl` 客户端在连接服务器时，总是尝试连接 `443` 端口，导致 `connection refused`。
*   **诊断**:
    *   初步推断为缺少 `insecure` 连接选项，但检查代码后发现该选项已正确设置。
    *   最终通过仔细分析代码和用户命令，发现 Bug 在于 `--server` 和 `--port` 两个参数的处理逻辑：当 `--server` 被设置时，`--port` 的值被忽略，导致 gRPC 客户端收到了一个没有端口的地址，从而错误地 fallback 到了 `443` 端口。
*   **修复**:
    *   **临时方案**: 指导用户将地址和端口合并到 `--server` 参数中，例如 `--server localhost:50051`。
    *   **长期方案**: 建议后续修改 `pcasctl` 代码，使其能正确组合 `--server` 和 `--port` 参数。

---

## 架构决策与澄清

*   **Docker 对 Go 应用的价值**: 明确了即使 Go 能编译为静态二进制文件，Docker 在提供环境一致性、依赖固化、标准化进程管理、安全隔离和简化网络配置等方面，依然具有不可替代的价值。
*   **D-App 职责划分**: 再次强调了 PCAS 生态中“能力提供方”（如 `DreamTrans`）和“能力消费方”（如 `Dreamscribe`）的清晰分离原则。提供方应保持通用和无状态，而消费方则负责处理业务逻辑和上下文。
*   **配置热更新**: 确认了 `policy.yaml` 当前不支持热更新，修改后需要重启容器 (`docker restart`) 才能生效。

这份文档记录了我们如何将 PCAS 的部署运维能力提升到一个新台阶的过程，为项目未来的稳定发展奠定了坚实的基础。