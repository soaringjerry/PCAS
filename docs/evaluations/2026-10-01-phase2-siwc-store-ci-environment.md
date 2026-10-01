# SIWC Store测试环境规则适配

首次本机3PASS报告及其执行harness5cb28bc不修改。协调者在CI整合审阅中指出CI已经使用`PCAS_TEST_DATABASE_URL`指向localhost:5432/pcas_test，而首次harness仅接受本机指定33273/phase2_c，会误拒绝合法隔离CI环境。

本改动只替换测试目标gate：必须显式非空PCAS_TEST_DATABASE_URL；scheme为postgres/postgresql；host为localhost或IP.IsLoopback；数据库名必须为_test后缀或phase2_前缀且无额外路径。仍逐例创建唯一schema，覆写search_path为己schema,public，cleanup只DROP己schema；业务断言完全保留，没有生产配置读取或skip。

本机不重跑3子例；只compile-only。组合头的真实CI由协调者执行，其证据须另记，不能以原本机3PASS冒称CI已通过。

`go test ./internal/ai/siwc -run '^$' -count=1`退出0；[原compile日志](2026-10-01-phase2-siwc-store-ci-environment-compile-original.log)。没有执行普通/claim/raw子例，没有改首次原报告。
