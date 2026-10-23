package retrieval

// What a recall says about its coverage, in the words that the model and
// the owner read.
const (
	GapNoVector       = "未配置语义索引；模糊措辞的覆盖尚不完整"
	GapPending        = "部分资料尚未完成索引或抽取；可通过原文继续追溯"
	FollowUpRelax     = "减少日期或属性限制，使用原文关键词再查"
	GapSourceBudget   = "部分原文超出上下文预算，请展开来源"
	GapRelationBudget = "关系展开达到候选预算，可继续展开相关对象"
	GapViaEpisode     = "此对象请通过关联的经历或原文查看"
)

// ContextLabel names the role and the branch of an original in a recall.
// An original without either has no label.
func ContextLabel(role, branch, text string) string {
	if role == "" && branch == "" {
		return text
	}
	return "[原文角色=" + role + "；分支=" + branch + "] " + text
}
