# M1 image acceptance expectations

Written before product implementation. All model responses, attachments and screenshots are synthetic. Existing tests remain unchanged.

| Sequence | Expected observations | Planned coverage |
| --- | --- | --- |
| V1 | Upload a notice, then submit its source reference with "帮我记一下". The secretary receives the vision text; creates a dated task; the original can be downloaded; a separate undoable "存了一张图片" receipt exists. | PostgreSQL + HTTP integration, browser |
| V2 | Submit only the image. The secretary receives its contents and responds; no storage-only acknowledgement. The user's original text stays empty. | PostgreSQL integration |
| V3 | Telegram downloads and stores the same synthetic notice, then calls DeskTurn with its source reference and caption, using the same image preparation as the web request. | Telegram adapter + shared PostgreSQL integration |
| V4 | No available vision protocol: retain the original, state "图片存好了，但我现在看不了图", execute caption-based actions, and allow background OCR with representation `ocr`. | PostgreSQL integration |
| V5 | Vision returns HTTP 500: same retention, fallback and caption behavior; no `vision` usage row for the failed call. | PostgreSQL integration |
| V6 | A standalone image import uses vision text, representation `vision`, and one `vision` usage row. Scanned PDF pages also prefer vision. Budget exhaustion uses OCR. | PostgreSQL integration |
| V7 | Undo only the image receipt: remove the original and derived material, preserving other actions. Repeated undo reports already undone; replay does not recreate the upload or call models. | PostgreSQL integration |
| V8 | Extraction gets user text as its only evidence source, with attachment understanding marked as assistant context. Reject a quotation solely from the image; keep a quotation from the caption. | PostgreSQL extraction integration |
| V9 | On home and thing pages: selection, paste and drop show removable attachment strips; send clears the draft and selection. Unsupported files and files over 20 MB show a local explanation and do not send. PDF/audio use the same upload and secretary path. | Mocked Playwright desktop/mobile |
| V10 | Inspect exact image payloads for Chat Completions, Responses, Anthropic, SIWC and Codex RPC. Codex support must be established by official documentation and the installed server schema, using a fake subprocess for requests. | Provider/manager/fake Codex tests |

Additional state sequences: same request replay; concurrent identical requests; an attachment assigned to a second turn; image undo before/after task undo; externally deleted attachment; owner isolation; independent imported images remain unchanged; image prompt injection is context, not authorization.
