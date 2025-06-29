# DreamHub 桌面启动器 MVP - 架构设计

## 1. 目标与范围

本项目旨在为 PCAS 平台创建一个最小化可行的桌面启动器 (Launcher MVP)。其核心目标是解决用户的根本痛点：**一键式地、可靠地安装与启动 PCAS 核心服务**。

此 MVP 版本将专注于以下核心功能，支持 Windows 和 macOS 平台。

## 2. 技术选型

- **核心框架**: `getlantern/systray`
  - **原因**: 这是一个专门用于创建系统托盘应用的轻量级 Go 库，跨平台支持良好，API 简单，非常适合我们的 MVP 需求。
- **进程管理**: Go 标准库 `os/exec`。
- **Docker 交互**: `docker` CLI (通过 `os/exec` 调用)。未来可考虑替换为 Docker Go SDK 以获得更好的集成。

## 3. 核心功能分解

### 3.1. 系统托盘菜单

启动器的主界面是一个系统托盘图标及其关联的菜单。

- **图标**: 需要设计一个品牌图标，并能通过状态变化（如动态图标）来反映 PCAS 服务的运行状态。
- **菜单项**:
  - `状态: [运行中/已停止/启动中/错误]` (动态文本，不可点击)
  - `---` (分隔线)
  - `启动 PCAS` (可点击，服务停止时可用)
  - `停止 PCAS` (可点击，服务运行时可用)
  - `---` (分隔线)
  - `打开 dApp 文件夹` (可点击)
  - `---` (分隔线)
  - `退出启动器` (可点击)

### 3.2. PCAS 服务管理

这是启动器的核心逻辑，负责管理 `pcas.exe` 和 `postgres` 容器的生命周期。

#### 启动流程

1.  用户点击 "启动 PCAS"。
2.  菜单项 "启动 PCAS" 变为不可用，"停止 PCAS" 变为可用。
3.  状态更新为 "启动中..."。
4.  **启动 Postgres 容器**:
    -   执行 `docker-compose up -d postgres` 命令。
    -   检查命令执行结果和容器健康状况。
5.  **启动 PCAS 核心**:
    -   执行 `pcas.exe` (或 `./pcas` for non-windows)。
    -   监控进程的 stdout/stderr 以便调试。
6.  **状态确认**:
    -   一旦两个组件都成功启动，状态更新为 "运行中"。
    -   如果任何一步失败，状态更新为 "错误"，并在日志中记录详细信息。

#### 停止流程

1.  用户点击 "停止 PCAS"。
2.  **停止 PCAS 核心**:
    -   向 `pcas.exe` 进程发送 `SIGTERM` 信号，实现优雅关闭。
3.  **停止 Postgres 容器**:
    -   执行 `docker-compose down` 命令。
4.  状态更新为 "已停止"。

### 3.3. 打开 dApp 文件夹

- 此功能提供一个便捷的入口，让用户可以轻松访问和管理他们的 dApp。
- 实现方式:
  - **Windows**: `exec.Command("explorer", dAppPath)`
  - **macOS**: `exec.Command("open", dAppPath)`

## 4. 项目结构

```
PCAS/
└── launcher/
    ├── main.go           # 程序入口，systray 初始化
    ├── pcas_manager.go   # 负责启动/停止 PCAS 核心和 Docker
    ├── platform/         # 平台特定的代码
    │   ├── platform_darwin.go
    │   └── platform_windows.go
    └── assets/           # 存放图标等资源
        └── icon.ico
        └── icon.png
```

## 5. 交互流程图

```mermaid
sequenceDiagram
    participant U as 用户
    participant L as 启动器 (Systray)
    participant PM as PCAS 管理器
    participant PG as Postgres (Docker)
    participant PC as PCAS 核心 (进程)

    U->>L: 点击 "启动 PCAS"
    L->>L: 更新菜单状态 (UI)
    L->>PM: 调用 StartPCAS()
    PM->>L: 更新状态: "启动中..."
    PM->>PG: docker-compose up -d
    activate PG
    PG-->>PM: 启动成功
    deactivate PG
    PM->>PC: ./pcas
    activate PC
    PC-->>PM: 启动成功
    deactivate PC
    PM->>L: 更新状态: "运行中"

    U->>L: 点击 "停止 PCAS"
    L->>PM: 调用 StopPCAS()
    PM->>PC: 发送 SIGTERM
    activate PC
    PC-->>PM: 进程退出
    deactivate PC
    PM->>PG: docker-compose down
    activate PG
    PG-->>PM: 容器停止
    deactivate PG
    PM->>L: 更新状态: "已停止"