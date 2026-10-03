# 任务 U1：大的归档分片上传

2026-10-03 用户反馈：上传 ChatGPT 导出文件一直提示「网络连接中断，读取这个文件没有完成」。由协调者直接实现。

## 原因

线上站点前面是 Cloudflare，它对单个请求的大小有上限（常见是 100 MB），超过的请求在到达服务器之前就被断开。服务器日志里没有这次上传的任何记录，页面只能看到连接断了。另外原来的做法是读取时传一遍、确认导入时再传一遍，同一个文件要完整上传两次。

## 规则

- 文件超过 32 MB 时，页面把它切成 8 MB 一片依次上传；32 MB 以内的照旧一次传完。
- 一片没传成（连接断了、服务器忙）会等一下重传这一片，最多连续失败 5 次才报错；服务器会告诉页面已经收到多少，从那里接着传。
- 读取（预览）和确认导入用的是服务器上已经收好的同一份，文件只上传一次。
- 服务器上的分片是临时文件：导入开始后删掉；两小时没动的删掉；同一个人最多同时留 3 份，再多就删最早的。服务重启后这些临时文件不保留，页面发现后会自动重传一次。
- 文件大小上限不变（200 MB，由 `connectors.MaxUploadBytes` 决定），超过时在开始上传前就提示太大。
- 只有主人能上传、读取、丢弃。

## 接口

- `POST /v1/connectors/archive/uploads`，`{ "name", "size" }` → `201 { "id", "pieceBytes" }`；太大返回 `413 archive_too_large`。
- `PUT /v1/connectors/archive/uploads/{id}?offset=N`，请求体是这一片的内容 → `{ "received" }`；`offset` 不等于已收到的长度时返回 `409 { "error": "upload_offset", "received" }`。
- `DELETE /v1/connectors/archive/uploads/{id}`。
- `POST /v1/connectors/archive/preview` 和 `POST /v1/connectors/archive` 除了原来的表单上传，也接受 JSON：`{ "upload": "<id>", "organize": "later|now" }`。

## 测试

- `internal/httpapi/archive_uploads_test.go`：按顺序收片、重复的片不重写、没收全不能读、超出声明大小的片被拒绝且不留在文件里、读取和导入拿到的内容和原文件一字节不差、导入后分片删除、不是主人不行。
- `web/tests/phase2-batch4.spec.ts` 最后一条：40 MB 的文件切成 5 片，第三片掉线一次后续传，读取和导入之间不重传。

## 还没解决的

- 超过 200 MB 的导出文件（带很多图片、语音的 ChatGPT 导出常常更大）仍然传不了。下一步是在浏览器里只取出对话文件上传，图片和语音不上传。
- 这台服务器的磁盘只剩约 7 GB，大文件导入要留意。
