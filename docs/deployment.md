# PCAS Deployment

Current operating instructions. Updated 2026-10-08.

The Compose deployment contains PostgreSQL, migration, API, and worker services.
The image includes the frontend, Go program, Codex CLI, PDF tools, OCR, and audio tools.
The [Dockerfile](../Dockerfile) defines installed versions.
The [Compose file](../compose.yml) defines services, volumes, and default values.

## 1 Initial Setup

1. Copy `.env.example` to `.env`.
2. Set a separate database password.
3. Set the owner UUID.
4. Set a random API token with at least 32 characters.
5. Set the file mode to `0600`.
6. Build and start the services.
7. Check readiness and logs.

```sh
cp .env.example .env
chmod 600 .env
docker compose build api
docker compose up -d --no-build
docker compose ps
curl -fsS http://127.0.0.1:12352/readyz
docker compose logs --tail=50 api worker migrate
```

Edit `.env` before starting services. Do not commit private settings.
The default address is `http://127.0.0.1:12352`.
For a public deployment, set the bind address and exact HTTPS origin:

```dotenv
PCAS_BIND_ADDRESS=0.0.0.0
PCAS_PORT=12352
PCAS_PUBLIC_URL=https://YOUR_HOST
```

The domain proxy supplies HTTPS. The public URL must match the browser origin.
The owner signs in with `PCAS_API_TOKEN`.
The service uses an HttpOnly session cookie. It does not save the token in browser storage.
Business routes need authentication, with the applicable connector-token exception.
Health routes and the login page are public.
External AI tokens use `PCAS_AGENT_TOKENS_FILE` and explicit record grants in a native deployment.

## 2 Codex Channel

1. Sign in to PCAS.
2. Open the Codex account settings.
3. Start device login.
4. Enter the displayed code on the supplied verification page.
5. Check the account status.
6. Select the subscription agent when necessary.

The configured default text model is `gpt-6.1-sol`.
PCAS keeps its Codex account in `PCAS_CODEX_HOME`, separate from developer accounts.
Compose uses `/var/lib/pcas/codex` in the persistent file volume.
The model thread is temporary and read-only. Shell and local file tools remain disabled.
Explicit search-capable turns can use web search. Background generation uses the applicable generation path.
See [Codex Adapter](../internal/ai/codex.go).
Subscription login does not configure vector or audio APIs.

## 3 API Models and Embeddings

Configure the text URL, key, model, and accounting prices in the API settings.
The text URL includes the provider's `/v1` path when the protocol requires it.
Select the default text provider separately from the embedding provider.
Configured prices support budget estimates. They are not provider quotations.

Compose stores settings in `/var/lib/pcas/model-api.json` with mode `0600`.
Native deployments use `PCAS_MODEL_SETTINGS_FILE`.
The API and worker read updated settings without a service restart.
A blank key preserves the old key only when the provider address is unchanged.

File-based provider definitions use `PCAS_MODELS_PATH` in Compose.
See [Model Configuration Example](../config/models.example.json).
The implementation supports OpenAI-compatible Chat Completions, Responses, and Anthropic Messages.
Local HTTP providers can use a supported protocol.
For file or environment changes, recreate the API and worker services.

```sh
docker compose up -d --force-recreate api worker
```

Embeddings use a separate key and provider.
The default embedding model is `text-embedding-3-small`; see the configuration example for the current dimension.
Use the rebuild control to queue missing vectors after a provider change.
Queries compare compatible provider, model, and dimension values.
Text retrieval remains available when vectors are missing.

The optional local service uses the `local-embeddings` profile:

```sh
docker compose --profile local-embeddings up -d embeddings
```

Then select that service in the private embedding configuration.
Audio transcription also needs its own configured provider.

## 4 ChatGPT Direct Channel

The direct channel is optional. Compose disables it unless `PCAS_CHATGPT_DIRECT_ENABLED=true`.
Codex remains the recorded default until the direct account meets the verification conditions.
The direct channel keeps separate credentials in `PCAS_CHATGPT_DIR`.
It does not reuse Codex authentication files.

For local login, use the direct-account control in settings.
The default callback is `http://127.0.0.1:1455/auth/callback`.
For remote login, the browser callback points to the user's computer.
The implemented settings flow accepts that complete callback address and finishes the pending login on the server.
See [Callback Handler](../internal/ai/siwc/manager.go) and [HTTP Routes](../internal/httpapi/chatgpt_direct.go).
The old statement that callback paste is unimplemented no longer applies.

If the model list does not contain a usable model, enter its name manually.
The selected account and provider determine whether the model can run.
Manual selection does not prove account capability.

A local helper and secure credential import remain alternatives:

```sh
go build -o bin/pcas ./cmd/pcas
./bin/pcas chatgpt-login --dir data/chatgpt --port 1455
./bin/pcas chatgpt-import --dir /YOUR/PRIVATE/chatgpt /YOUR/PRIVATE/transfer.json
```

Transfer credentials through a protected channel. Preserve private file permissions.
Do not expose access tokens or refresh tokens in browser responses, chat, or logs.
Do not revoke a transferred session on the helper machine while the server still needs it.

Complete lifecycle verification requires generation, refresh, generation after refresh, and remote revocation:

```sh
./bin/pcas chatgpt-verify --dir data/chatgpt
# Compose alternative:
docker compose exec -T api pcas chatgpt-verify --dir /var/lib/pcas/chatgpt
```

**This command revokes the account session. Reconnect after successful verification.**
If refresh is not yet permitted, use the next time reported by the command.
The existing record includes login and generation, but not complete real-account lifecycle verification.
The [Historical Account Record](history/chatgpt-plan-auth.md) preserves earlier decisions and test details.

## 5 Notifications and Attachments

Configure reminders in settings.
Web Push needs the applicable browser permission and HTTPS deployment.
Configure the Telegram bot and send it a private message before linking the chat.
Telegram voice input needs transcription configuration.
Notification credentials are in `notify.json`; `PCAS_NOTIFY_SETTINGS_FILE` can select another private path.
A server send result does not establish that a phone displayed the notification.

Ordinary attachments and archives have different size limits.
See the [Input Reference](connectors.md#implemented-limits).
Parsing preserves the original and exposes missing or failed representations.
A JSON export does not include binary attachments or model credentials.

## 6 Upgrade and Backup

Before an authorized release, check the target revision and backup location.

1. Create a database backup.
2. Check that the command succeeded and the backup file is nonempty.
3. Preserve the required file-volume and private-configuration backups.
4. Build the approved revision.
5. Start services after migration succeeds.
6. Check readiness and logs.
7. Complete the live checks.

```sh
docker compose exec -T db pg_dump -U pcas -d pcas -Fc > pcas-backup.dump
test -s pcas-backup.dump
docker compose build api
docker compose up -d
curl -fsS http://127.0.0.1:12352/readyz
docker compose logs --tail=50 api worker migrate
```

Use a unique backup filename for each release. Keep backups in a protected location.
Preserve both database and file volumes, private model settings, notification settings, account files, and `.env`.
Do not edit migrations that have been deployed.
Do not run `docker compose down -v` during an upgrade.
For token rotation, change `PCAS_API_TOKEN` and recreate the API service.

## 7 Live Checks

Test the secretary through the real default Codex channel after deployment.
Use an answer, an arrangement, and a change when those actions apply to the release.
Check the executed objects, receipts, and errors.
Record claim counts before and after cleanup. Attribute test records by request or source ID.
Do not select production cleanup records by creation time.

The owner Bearer token can use a new `smokeId` for a check-only conversation.
Use distinct request IDs within that group. Ordinary browser sessions and agent tokens cannot use this marker.
The check executes supported item actions without creating normal memory sources.
Attachments are not supported. Memory date closure is skipped in check mode.
A skipped action does not prove normal-mode behavior.

Clean the group with `DELETE /v1/desk/smoke/{smokeId}`.
Check the response. A group changed by outside work can return `409` without cleanup.
Existing production memory is outside the group's cleanup scope.
Check usage records and relevant sources as well as counts.
External notifications that were delivered cannot be undone by local cleanup.
The [Phase 3.6 Record](evaluations/2026-10-08-phase3_6-rollout.md) describes the known check-mode reply discrepancy.
