# Sign in with ChatGPT 套餐授权

此通道按 OpenAI 官方开源应用流程实现，适用于本机和个人自托管部署。它使用你的 ChatGPT 套餐及账户允许的额度，直接请求 `https://api.openai.com/v1/responses`；不启动 Codex 子进程。原有 Codex App Server 登录、API Key、PCAS 登录、记忆与事项均保留。

目前继续使用 Codex 订阅接入。新直连通道默认关闭，设置页不展示；仅在明确设置 `PCAS_CHATGPT_DIRECT_ENABLED=true` 后启用开发验证入口。即使已有直连凭据，关闭此开关也不会自动切换到新通道。独立 CLI 辅助命令仍可用于开发验证。

## 暂缓原因与当前决策（2026-09-29）

**当前个人远程自托管场景下，这个直连通道的授权接入太麻烦，暂时继续用 Codex。**这是针对 PCAS 当前部署和使用体验的判断，本机部署不需要同样的凭据转移步骤。

具体原因：

- 官方 OSS 套餐授权使用 `127.0.0.1` 本机回调。浏览器在用户电脑上、PCAS 在远程服务器上时，回调无法直接到达服务器，不能直接套用网站公网回调的登录方式。[官方登录要求](https://developers.openai.com/siwc/token-sharing-open-source/sign-in)
- 官方远程 VM 指南要求先在本机完成 OAuth，再通过 SSH 等安全通道转移受保护凭据，之后由 VM 管理刷新。对用户而言，增加了本机授权、凭据传输和服务器导入等步骤。[官方 VM 流程](https://developers.openai.com/siwc/token-sharing-open-source/self-hosted-vms)
- 单独提供 Windows/macOS/Linux 授权辅助程序是本项目曾考虑的实现方案，官方没有要求必须安装专用辅助程序；浏览器页面本身也不需要按这些操作系统分别开发。辅助程序会额外增加分发和使用成本。
- 用户提出的“手动复制完整回调地址，再粘贴回 PCAS”可能简化体验，但当前尚未实现，官方文档也没有明确给出该方式，需要真实账户验证后才能确认可用。

直连的收益是直接调用 Responses API、减少 Codex 子进程依赖；目前这些收益不足以抵消远程授权的复杂度。现有 Codex 设备验证码登录对当前部署更方便。

因此，保留现有 Codex 和 API Key 通道；直连开发暂缓，代码保留但默认关闭，不迁移旧凭据，也不继续扩展各操作系统的辅助程序。自动化回归已通过，但真实 OpenAI 登录、生成、刷新和撤销尚未完整验收。以后如要重新推进，应先验证更简单的授权体验，再完成真实生命周期验收，最后决定是否切换默认。

## 2026-10-03 更新（三）：模型可以手动填写

模型列表接口（`/models`）可能不列出账户实际能用的模型（用户反馈：6.1 Sol 不在列表里但能用），列表读取失败时还会让设置页整栏显示失败、每次生成都失败。现在：

- 设置页多了「手动填写模型名」。填的名字只检查格式（字母、数字、`.`、`_`、`-`，最长 64），连同一个「手动填写」标记（`model_manual`）存在账户上。
- 用手动填写的模型生成时不再读模型列表，直接把名字发给 OpenAI；账户用不了这个模型时，由 OpenAI 返回的错误说明。
- 从列表里选模型仍然按列表校验，并清掉「手动填写」标记。没有填过模型时，仍然取列表里的第一个。
- 列表读不到时设置页不再整栏报错，只是不显示下拉。
- 上线：2026-10-03（main `9d7db37`）。线上开关 `PCAS_CHATGPT_DIRECT_ENABLED=true` 也在同一天打开。真实账户登录、填 6.1 Sol 能否生成，还没有验证。

## 2026-10-03 更新（二）：远程部署直接在设置页完成登录

远程部署时，浏览器在用户电脑上，回调地址 `http://127.0.0.1:1455/auth/callback` 指向的是用户电脑自己，够不到服务器，所以授权后浏览器会停在一个「无法访问」的页面。现在设置页在等待授权时多了一个输入框：把地址栏里的完整地址粘贴进去，点「完成登录」，服务器用它完成换取凭据。

- 接口：`POST /v1/chatgpt/direct/callback`，只有主人能调用，请求体 `{"url": "..."}`。
- 检查和自动回调完全一样（`Manager.Complete` 复用同一段处理）：地址必须和本次登录的回调地址一致，`state` 必须对得上，一次登录只能用一次，十分钟内有效；换取凭据仍然带着只存在服务器内存里的 PKCE 校验值。地址填错不会作废这次登录，可以重新粘贴。
- 这样不需要下载助手，也不需要在两台机器之间传凭据文件：长期凭据从头到尾只在服务器上生成和保存，经过网页的只有一次性的授权码。
- 和官方远程 VM 指南的区别：官方做法是本机完成授权后经 SSH 转移凭据。这里改成把一次性授权码经 PCAS 自己的 HTTPS 页面交给服务器。下面的 Windows 助手和 SSH 导入流程仍然保留，作为备选。

## 2026-10-03 更新：重新试用，并提供 Windows 授权助手

用户决定试用这条接入，用途是**后台整理**（抽取）：它不起 Codex 子进程、能并发、返回 token 用量，适合大批量的导入和补做。秘书仍然用 Codex，因为秘书需要强制输出格式和联网搜索，这条接入现在的实现没有这两样。实测对比（速度、准确率、用量）由协调者在登录完成后用评测工具跑，结果另行记录；在那之前上面「暂缓」一节的判断仍然是当前事实。

为了让远程部署的授权少走几步，新增一个独立的本机授权助手 `cmd/pcas-chatgpt-login`，可以编译成 Windows 的 exe：

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o pcas-chatgpt-login.exe ./cmd/pcas-chatgpt-login
```

它只做「本机登录」这一步，和 `pcas chatgpt-login` 是同一套代码：在本机监听 `127.0.0.1:1455`，打开浏览器，完成授权后把受保护的 `credentials.json` 写到用户目录下的 `pcas-chatgpt` 文件夹，并提示下一步。双击运行即可；重新授权时粘贴上次签发的客户端 ID。之后的传输和导入仍按下面「个人远程 Docker／VM」一节。

为此 `internal/ai/siwc` 加了一层很薄的平台适配（`platform_unix.go`、`platform_windows.go`）：文件锁、不跟随符号链接、权限位检查、目录同步。Linux 和 macOS 上的行为不变；Windows 上用系统的文件锁，权限靠用户目录的访问控制而不是权限位。Windows 版只用于这个本机助手，服务端仍然只在 Linux 上运行。

## 官方依据

核对日期：2026-09-29。

- [概览与主机 ID](https://developers.openai.com/siwc/token-sharing-open-source)
- [动态注册、PKCE、回调和身份验证](https://developers.openai.com/siwc/token-sharing-open-source/sign-in)
- [账户、刷新和退出撤销](https://developers.openai.com/siwc/token-sharing-open-source/profiles-and-sessions)
- [可用模型与流式生成](https://developers.openai.com/siwc/token-sharing-open-source/models-and-inference)
- [个人远程 VM](https://developers.openai.com/siwc/token-sharing-open-source/self-hosted-vms)
- [当前参数限制](https://developers.openai.com/siwc/token-sharing-open-source/preview-limitations)
- [错误及额度恢复](https://developers.openai.com/siwc/token-sharing-open-source/errors-and-recovery)

## 本机登录

原生部署默认凭据目录为 `data/chatgpt`，可通过 `PCAS_CHATGPT_DIR` 指定。回调使用 `http://127.0.0.1:1455/auth/callback`；可设置 `PCAS_CHATGPT_CALLBACK_PORT`，只改变端口，不改变回调 scheme、主机或路径。设置页的 **Continue with ChatGPT** 打开官方授权页，完成后自动更新账户。

Compose 在容器内监听 `0.0.0.0:1455`，仅把回调端口发布到宿主机 `127.0.0.1`。回调请求仍严格校验 `Host: 127.0.0.1:1455`。新版本需要重建镜像和重建容器以应用端口映射：

```sh
docker compose build api
docker compose up -d --no-build
```

使用其他本机端口时，例如在 `.env` 设置 `PCAS_CHATGPT_CALLBACK_PORT=1456`，Compose 会同步修改监听和映射端口。辅助命令使用同一端口时传入 `--port 1456`。

也可以独立运行授权辅助命令，无需数据库、API Key 或 Codex：

```sh
go build -o bin/pcas ./cmd/pcas
./bin/pcas chatgpt-login --dir data/chatgpt --port 1455
# 之后重新授权同一账户：使用设置页显示的签发客户端 ID。
./bin/pcas chatgpt-login --dir data/chatgpt --client oaiapp_YOUR_ISSUED_ID
```

首次使用 `dynamic_agent_client` 注册，之后复用签发的客户端 ID。每次有新的 state、nonce、PKCE；ID token 验证签名、issuer、audience、有效期和 nonce。两个工作区即使 email 相同也保留独立注册。身份登录成功但未授予 `chatgpt.tokens.use.direct` 时保留连接，禁止套餐生成；设置页可明确请求重新同意套餐权限。

## 个人远程 Docker／VM

浏览器的 `127.0.0.1` 指向本机。不要把远程 HTTPS 域名作为该 OSS 流程的回调。当前实现按官方 VM 流程在浏览器所在本机授权，安全转移受保护凭据，之后由 VM 负责刷新。手动粘贴回调地址的简化方案尚未实现或通过真实账户验证。

1. 在本机使用本次 PCAS 源码构建并执行上面的 `chatgpt-login`。Linux/macOS 原生命令均支持；也可以使用本机 Docker 配合 loopback 端口映射。
2. 通过 SSH/SCP 把本机的 `data/chatgpt/credentials.json` 传到服务器的私有临时路径，保持 `0600`。不要通过聊天、网页或日志传递令牌。
3. 原生 VM 导入：`pcas chatgpt-import --dir /YOUR/PRIVATE/chatgpt /YOUR/PRIVATE/transfer.json`。
4. Docker 可把文件以只读挂载交给专用辅助容器；文件须由容器 UID 10001 可读，目录和文件仍只允许所有者访问：

```sh
# 在服务器项目目录执行；transfer.json 来自 SSH 安全传输。
sudo install -o 10001 -g 10001 -m 600 /YOUR/PRIVATE/transfer.json data/chatgpt-transfer.json
docker compose run --rm --no-deps \
  -v "$PWD/data/chatgpt-transfer.json:/tmp/chatgpt-transfer.json:ro" \
  api chatgpt-import --dir /var/lib/pcas/chatgpt /tmp/chatgpt-transfer.json
```

导入验证签名身份和账户模型访问，保留 VM 自己生成的主机 ID，绝不用拷贝的本机 ID 覆盖。辅助本机授权进程完成后退出，不继续刷新同一会话；不要在本机退出撤销刚转移的会话，否则 VM 也会失去授权。该流程转移现有会话，官方暂未提供转移会话的主机级用量归属和撤销能力。

## 完整验收后才默认

新通道可显式选择，未完成真实生命周期验证前不会成为默认。不会迁移、读取或复用 Codex 的认证文件。

完成首次真实 OAuth 后，在持有该凭据的运行环境执行：

```sh
# 原生本机／VM
./bin/pcas chatgpt-verify --dir data/chatgpt
# 已运行本次镜像的 Docker
docker compose exec -T api pcas chatgpt-verify --dir /var/lib/pcas/chatgpt
```

命令依次调用无记忆的简单生成、旋转刷新、刷新后的生成和远端撤销。若 `earliest_refresh_at` 尚未到达，命令会给出可刷新时间，需等到该时间再执行。每次成功都必须收到 `response.completed`；撤销必须返回 HTTP 200。验证成功会退出该会话，再次用相同签发客户端 ID 登录后才启用默认。远程部署需本机重新授权并再次导入；导入保留目标已完成的验收状态。

默认影响未指定副手的选择，以及原来默认为 `chatgpt` 的后台抽取；现有明确选择的副手及自定义 API 提供者保持原选择。新副手首次创建时继承原 ChatGPT 副手的记忆种类、推断设置和已有可见授权，之后各自设置独立，撤权不会被重新补回。

## 凭据和请求边界

`credentials.json` 和独立锁文件使用 `0600`，目录为 `0700`，写入采用原子替换和同步。API、worker、辅助命令共用进程间锁，串行刷新旋转令牌。正常网络失败保留凭据；终止性刷新错误清除令牌但保留注册映射。退出先尝试撤销刷新令牌，临时失败有限重试，之后清除本地令牌并明确显示远端撤销是否确认。

只向官方 OAuth endpoint 和公开模型/Responses endpoint 发送凭据；禁止重定向转发。浏览器只得到账户摘要和授权 URL，没有 access/refresh token。模型目录从同一账户令牌取得，显示 `visibility=list` 的 `display_name`，请求使用 `slug`，不复用 Codex 模型缓存。

HTTP 推理使用数组 `input`、`instructions`、`store:false` 和 `stream:true`，不发送现有 API Key Responses 的 `max_output_tokens` 或其他不支持字段。完整上下文由 PCAS 原流程组装。流中失败、未完成和断流不会保存为成功结果。额度限制暂停此套餐通道并链接到 [ChatGPT 用量管理](https://chatgpt.com/settings/usage)，用户调整后显式恢复，不自动切换其他计费通道。向量及音频仍使用独立配置。

## 验证记录

自动回归覆盖真实形态的本机回调、PKCE 交换、JWKS 验签、错误身份/nonce/回调拒绝、并发旋转刷新、身份与套餐权限分离、完整 SSE/流后额度失败、撤销失败、导入时主机隔离以及 HTTP 管理权限。回归中的 OAuth/模型服务使用本机测试服务器，不能替代 OpenAI 真实账户验收。

真实账户生命周期验收状态保存在每条注册的 `verified` 字段；没有成功记录时不启用默认。

本次开发验证：`make check` 在独立临时 PostgreSQL/pgvector 数据库上通过，前端 lint、类型检查、构建通过，三个 Playwright 场景通过，Docker 镜像构建通过。真实 OpenAI OAuth、套餐生成、刷新与撤销尚未执行，需要用户在本机浏览器完成专用授权；没有启用新默认，也没有重启生产服务。
