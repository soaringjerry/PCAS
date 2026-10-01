# 真实 manual harness：有证据的 selector 单点修正

原红报告 `1dea95380eb7dc892d8f516b663bb8181936dd67` 保留：产品 dc766ca1，原 spec2073622d，两例因 exact accessible name 不符超时。root 批准仅修这一定位并先净交审，动态另指定头。

`controls.tsx:156` 是 `aria-label={`${label}：${current?.label ?? placeholder}`}`，真实两份error-context/trace可见“转交接收者：选择接收者”。新 spec line106 从 exact `转交接收者` 改为 exact `转交接收者：选择接收者`，继续只在有本次请求textbox的 form 内找button；保真实option/name、request payload、包/manifest、clipboard、精确409和完成/owner断言。未改产品label，不使用force或宽泛定位。

新 spec SHA256 `84e75a62eeb73db8d13f5a9df0f063349ad04446ab17bf1c53169f43b5a78606`。ESLint exit0、strict targeted TS exit0、Playwright --list exit0仅2 tests/1file，gitdiffcheck0；命令和Node24/tmp真实Node typeRoots与初次静态报告一致。原日志见selector-static-evidence/。没有动态重跑。

业务冻结3df40499未变，产品仍dc766ca1，只harness发生明确可审的一行修正；待root最终head/执行批准。同源facade真实200和catalog200已证明的前次行为不改写成整体闭环通过。临时node_modules借用symlink已移除。
