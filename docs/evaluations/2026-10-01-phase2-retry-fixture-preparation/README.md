# 六个首红：有因夹具修订准备

原首轮固定834949f的23叶17P6F及完整raw不改，见[首红报告](../2026-10-01-phase2-retry-causal-r1/README.md)。追加[gold erratum](../../../testdata/phase2/retry-causal-fixture-erratum.json)独立commit a6c461a；原13/manual/receipt/query gold均保持。root和D独立审批准以下四项，产品不变。

- Undoable保历史动作capability含义；不再误要求false，严格Undone=true/真实两次Undo/原ActionID/empty两title与ThingID保留。
- SOURCE先保source仍获准的合法first历史；second真实继承已撤create必须generic、ThingID=nil、无atom、Reply/Cards隐藏。正式撤原source policy后整个HTTP无atom/title，两个ActionID/Undone仍存在。真实Input/两个action context_task和typed rows在Undo前输出，补齐首红Fatal前缺失直接记录。
- studio用实际THIS项目alias（真实req.ThingID=A和实际serialized prompt所见THIS项目名）create_task(project=THIS)→delegate:N1；两receipt实际成功，真实Task ProjectID=A而非委派给project自身。secretary实际manifest scope与deputy Task scope均严格A，Promptsubset恰实际delegate1，general实际create+delegate2；新manifest与旧general集合**精确相等、无重复/额外**，普通单delegate仍exact1。B仍给真实canonical allow但仅A assignment，移动同Task再拒供。
- 仅delete两叶改严格ErrNotFound：先证明真实503旧Run存在、删除后旧Run/source/versions/record0、opaque tombstone增加；同source正式Ingest严格ErrBlocked。旧attempt exact invalidated/deleted且snapshot ErrUnavailable/空；之后新Run0、新attempt0、provider/embedding0。其它revoke/correct保持严格Conflict。

仅编译、无动态。下一root指定组合后只选原6红（2顶层6叶），已17P不本地重跑。新集合的完整23叶及旧全套由最终同头CI另验证。
