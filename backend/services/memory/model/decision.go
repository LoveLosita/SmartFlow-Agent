package model

// RelationType 常量描述一条新 fact 与一条旧记忆之间的关系。
//
// 四种关系：
// 1. duplicate — 完全重复，新 fact 没有新信息，旧记忆无需变动；
// 2. update — 新 fact 是对旧记忆的修正、补充或更精确表述，需要合并更新；
// 3. conflict — 新 fact 与旧记忆矛盾，旧记忆已过时，应删旧增新；
// 4. unrelated — 两者说的是不同的事情，互不影响。
const (
	RelationDuplicate = "duplicate"
	RelationUpdate    = "update"
	RelationConflict  = "conflict"
	RelationUnrelated = "unrelated"
)

// CandidateSnapshot 是喂给 LLM 的旧记忆候选快照。
//
// 职责边界：
// 1. 只承载 LLM 做关系判断所需的最小信息；
// 2. MemoryID 是真实 memory_id，LLM 不可见，仅供汇总决策时使用；
// 3. Score 是向量召回的相似度分数，用于多条 update 时选最优候选。
type CandidateSnapshot struct {
	MemoryID   int64
	Title      string
	Content    string
	MemoryType string
	Score      float64 // Milvus 相似度分数（0 表示来自 Hash 查询）
}

// ComparisonResult 是单次"新 fact vs 一条旧记忆"的 LLM 输出。
//
// 职责边界：
// 1. 只描述 LLM 对一对比较的结果，不包含最终决策动作；
// 2. UpdatedContent/UpdatedTitle 仅在 relation=update 时有意义；
// 3. Reason 写审计日志用，便于后续复盘 LLM 判断依据。
type ComparisonResult struct {
	MemoryID       int64  // 被比较的旧记忆 ID
	Relation       string // duplicate / update / conflict / unrelated
	UpdatedContent string // 仅 relation=update 时有意义：合并后的新内容
	UpdatedTitle   string // 仅 relation=update 时有意义：合并后的新标题
	Reason         string // 判断理由（写审计日志用）
}

// FinalDecision 是汇总后的最终动作。
//
// 说明：
// 1. 由确定性代码产出，不是 LLM 产出；
// 2. Action 取值复用 status.go 中已定义的 DecisionActionAdd/Update/Delete/None 常量；
// 3. TargetID 在 UPDATE/DELETE 时指向旧记忆 ID，ADD/NONE 时为 0。
type FinalDecision struct {
	Action   string // ADD / UPDATE / DELETE / NONE
	TargetID int64  // UPDATE/DELETE 时指向旧记忆 ID
	Title    string // UPDATE 时的新标题
	Content  string // UPDATE 时的新内容
	Reason   string // 汇总理由
}

// UpdateContentFields 是 UPDATE 动作需要更新的字段集合。
//
// 说明：
// 1. 只包含 UPDATE 动作实际需要修改的字段，避免全量覆盖；
// 2. NormalizedContent/ContentHash 由调用方重新计算，保证一致性。
type UpdateContentFields struct {
	Title             string
	Content           string
	NormalizedContent string
	ContentHash       string
	Confidence        float64
	Importance        float64
}
