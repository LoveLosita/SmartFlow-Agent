package agentmodel

import (
	"sort"
	"strings"
	"time"

	agentshared "github.com/LoveLosita/smartflow/backend/agent2/shared"
	"github.com/LoveLosita/smartflow/backend/model"
)

const (
	datetimeLayout = agentshared.MinuteLayout

	ScheduleRefineDefaultPlanMax        = 2
	ScheduleRefineDefaultExecuteMax     = 24
	ScheduleRefineDefaultPerTaskBudget  = 4
	ScheduleRefineDefaultReplanMax      = 2
	ScheduleRefineDefaultCompositeRetry = 2
	ScheduleRefineDefaultRepairReserve  = 1
)

const (
	defaultPlanMax        = ScheduleRefineDefaultPlanMax
	defaultExecuteMax     = ScheduleRefineDefaultExecuteMax
	defaultPerTaskBudget  = ScheduleRefineDefaultPerTaskBudget
	defaultReplanMax      = ScheduleRefineDefaultReplanMax
	defaultCompositeRetry = ScheduleRefineDefaultCompositeRetry
	defaultRepairReserve  = ScheduleRefineDefaultRepairReserve
)

// RefineContract 琛ㄧず鏈疆寰皟鎰忓浘濂戠害銆?
type RefineContract struct {
	Intent            string            `json:"intent"`
	Strategy          string            `json:"strategy"`
	HardRequirements  []string          `json:"hard_requirements"`
	HardAssertions    []RefineAssertion `json:"hard_assertions,omitempty"`
	KeepRelativeOrder bool              `json:"keep_relative_order"`
	OrderScope        string            `json:"order_scope"`
}

// RefineAssertion 琛ㄧず鍙敱鍚庣鐩存帴鍒ゅ畾鐨勭粨鏋勫寲纭柇瑷€銆?
//
// 瀛楁璇存槑锛?
// 1. Metric锛氭柇瑷€鎸囨爣鍚嶏紝渚嬪 source_move_ratio_percent锛?
// 2. Operator锛氭瘮杈冩搷浣滅锛屾敮鎸?== / <= / >= / between锛?
// 3. Value/Min/Max锛氶槇鍊硷紱
// 4. Week/TargetWeek锛氬彲閫夊懆娆′笂涓嬫枃銆?
type RefineAssertion struct {
	Metric     string `json:"metric"`
	Operator   string `json:"operator"`
	Value      int    `json:"value,omitempty"`
	Min        int    `json:"min,omitempty"`
	Max        int    `json:"max,omitempty"`
	Week       int    `json:"week,omitempty"`
	TargetWeek int    `json:"target_week,omitempty"`
}

// HardCheckReport 琛ㄧず缁堝纭牎楠岀粨鏋溿€?
type HardCheckReport struct {
	PhysicsPassed bool     `json:"physics_passed"`
	PhysicsIssues []string `json:"physics_issues,omitempty"`

	IntentPassed bool     `json:"intent_passed"`
	IntentReason string   `json:"intent_reason,omitempty"`
	IntentUnmet  []string `json:"intent_unmet,omitempty"`

	OrderPassed bool     `json:"order_passed"`
	OrderIssues []string `json:"order_issues,omitempty"`

	RepairTried bool `json:"repair_tried"`
}

// ReactRoundObservation 璁板綍姣忚疆 ReAct 鐨勫叧閿瀵熴€?
type ReactRoundObservation struct {
	Round         int            `json:"round"`
	GoalCheck     string         `json:"goal_check,omitempty"`
	Decision      string         `json:"decision,omitempty"`
	ToolName      string         `json:"tool_name,omitempty"`
	ToolParams    map[string]any `json:"tool_params,omitempty"`
	ToolSuccess   bool           `json:"tool_success"`
	ToolErrorCode string         `json:"tool_error_code,omitempty"`
	ToolResult    string         `json:"tool_result,omitempty"`
	Reflect       string         `json:"reflect,omitempty"`
}

// PlannerPlan 琛ㄧず Planner 鐢熸垚鐨勯樁娈垫墽琛岃鍒掋€?
type PlannerPlan struct {
	Summary string   `json:"summary"`
	Steps   []string `json:"steps,omitempty"`
}

// RefineSlicePlan 琛ㄧず鍒囩墖鑺傜偣杈撳嚭銆?
type RefineSlicePlan struct {
	WeekFilter      []int  `json:"week_filter,omitempty"`
	SourceDays      []int  `json:"source_days,omitempty"`
	TargetDays      []int  `json:"target_days,omitempty"`
	ExcludeSections []int  `json:"exclude_sections,omitempty"`
	Reason          string `json:"reason,omitempty"`
}

// RefineObjective 琛ㄧず鈥滃彲鎵ц涓斿彲鏍￠獙鈥濈殑鐩爣绾︽潫銆?
//
// 璁捐璇存槑锛?
// 1. 鐢?contract/slice 浠庤嚜鐒惰瑷€缂栬瘧寰楀埌锛?
// 2. 鎵ц闃舵锛坉one 鏀跺彛锛変笌缁堝闃舵锛坔ard_check锛夊叡鐢ㄥ悓涓€浠界害鏉燂紱
// 3. 閬垮厤鈥滄墽琛岄€昏緫涓庣粓瀹￠€昏緫鍚勮鍚勮瘽鈥濄€?
type RefineObjective struct {
	Mode string `json:"mode,omitempty"` // none | move_all | move_ratio

	SourceWeeks []int `json:"source_weeks,omitempty"`
	TargetWeeks []int `json:"target_weeks,omitempty"`
	SourceDays  []int `json:"source_days,omitempty"`
	TargetDays  []int `json:"target_days,omitempty"`

	ExcludeSections []int `json:"exclude_sections,omitempty"`

	BaselineSourceTaskCount int `json:"baseline_source_task_count,omitempty"`
	RequiredMoveMin         int `json:"required_move_min,omitempty"`
	RequiredMoveMax         int `json:"required_move_max,omitempty"`

	Reason string `json:"reason,omitempty"`
}

// ScheduleRefineState 鏄繛缁井璋冨浘鐨勭粺涓€鐘舵€併€?
type ScheduleRefineState struct {
	// 1) 璇锋眰涓婁笅鏂?
	TraceID        string
	UserID         int
	ConversationID string
	UserMessage    string
	RequestNow     time.Time
	RequestNowText string

	// 2) 缁ф壙鑷瑙堝揩鐓х殑鏁版嵁
	TaskClassIDs []int
	Constraints  []string
	// InitialHybridEntries 淇濆瓨鏈疆寰皟寮€濮嬪墠鐨勫熀绾匡紝鐢ㄤ簬缁堝鍋氣€滃墠鍚庡姣斺€濄€?
	// 璇存槑锛?
	// 1. 鍙璇箟锛屼笉鍙備笌鎵ц鏈熸敼鍐欙紱
	// 2. 缁堝鍙熀浜庡畠鍒ゆ柇鈥滄潵婧愪换鍔℃槸鍚︾湡姝ｈ縼绉诲埌鐩爣鍖哄煙鈥濄€?
	InitialHybridEntries []model.HybridScheduleEntry
	HybridEntries        []model.HybridScheduleEntry
	AllocatedItems       []model.TaskClassItem
	CandidatePlans       []model.UserWeekSchedule

	// 3) 鏈疆鎵ц鐘舵€?
	UserIntent string
	Contract   RefineContract

	PlanMax       int
	PerTaskBudget int
	ExecuteMax    int
	ReplanMax     int
	// CompositeRetryMax 琛ㄧず澶嶅悎璺敱澶辫触鍚庣殑鏈€澶ч噸璇曟鏁帮紙涓嶅惈棣栨灏濊瘯锛夈€?
	CompositeRetryMax int

	PlanUsed   int
	ReplanUsed int

	MaxRounds     int
	RepairReserve int
	RoundUsed     int
	ActionLogs    []string

	ConsecutiveFailures int
	ThinkingBoostArmed  bool
	ObservationHistory  []ReactRoundObservation

	CurrentPlan      PlannerPlan
	BatchMoveAllowed bool
	// DisableCompositeTools=true 琛ㄧず宸茶繘鍏?ReAct 鍏滃簳锛岀姝㈠啀璋冪敤澶嶅悎宸ュ叿銆?
	DisableCompositeTools bool
	// CompositeRouteTried 鏍囪鏄惁灏濊瘯杩団€滃鍚堟壒澶勭悊璺敱鈥濄€?
	CompositeRouteTried bool
	// CompositeRouteSucceeded 鏍囪澶嶅悎鎵瑰鐞嗚矾鐢辨槸鍚﹀凡瀹屾垚鈥滃鍚堝垎鏀嚭绔欌€濄€?
	//
	// 璇存槑锛?
	// 1. true 琛ㄧず褰撳墠閾捐矾鍙互璺宠繃 ReAct 鍏滃簳锛岀洿鎺ヨ繘鍏?hard_check锛?
	// 2. 瀹冧笉绛変环浜庘€滅粓瀹″凡閫氳繃鈥濓紝缁堝鏄惁閫氳繃浠嶄互鍚庣画 HardCheck 缁撴灉涓哄噯锛?
	// 3. 杩欐牱鍖哄垎鏄负浜嗛伩鍏嶁€滃鍚堝伐鍏峰凡鎴愬姛鎵ц锛屼絾涓氬姟鐩爣瑕佺瓑缁堝瑁佸喅鈥濇椂琚鍒や负澶辫触銆?
	CompositeRouteSucceeded bool
	TaskActionUsed          map[int]int
	EntriesVersion          int
	SeenSlotQueries         map[string]struct{}

	// RequiredCompositeTool 琛ㄧず鏈疆绛栫暐瑕佹眰鈥滃繀椤昏嚦灏戞垚鍔熶竴娆♀€濈殑澶嶅悎宸ュ叿銆?
	// 鍙栧€肩害瀹氾細"" | "SpreadEven" | "MinContextSwitch"銆?
	RequiredCompositeTool string
	// CompositeToolCalled 璁板綍澶嶅悎宸ュ叿鏄惁鑷冲皯璋冪敤杩囦竴娆★紙涓嶅尯鍒嗘垚鍔熷け璐ワ級銆?
	CompositeToolCalled map[string]bool
	// CompositeToolSuccess 璁板綍澶嶅悎宸ュ叿鏄惁鑷冲皯鎴愬姛杩囦竴娆°€?
	CompositeToolSuccess map[string]bool

	SlicePlan          RefineSlicePlan
	Objective          RefineObjective
	WorksetTaskIDs     []int
	WorksetCursor      int
	CurrentTaskID      int
	CurrentTaskAttempt int

	LastFailedCallSignature string
	OriginOrderMap          map[int]int

	// 4) 缁堝鐘舵€?
	HardCheck HardCheckReport

	// 5) 鏈€缁堣緭鍑?
	FinalSummary string
	Completed    bool
}

// NewScheduleRefineState 鍩轰簬涓婁竴鐗堥瑙堝揩鐓у垵濮嬪寲鐘舵€併€?
//
// 鑱岃矗杈圭晫锛?
// 1. 璐熻矗鍒濆鍖栭绠椼€佷笂涓嬫枃瀛楁涓庡彲鍙樼姸鎬佸鍣紱
// 2. 璐熻矗鎷疯礉 preview 鏁版嵁锛岄伩鍏嶈法璇锋眰寮曠敤姹℃煋锛?
// 3. 涓嶈礋璐ｅ仛浠讳綍璋冨害鍔ㄤ綔銆?
func NewScheduleRefineState(traceID string, userID int, conversationID string, userMessage string, preview *model.SchedulePlanPreviewCache) *ScheduleRefineState {
	now := nowToMinute()
	st := &ScheduleRefineState{
		TraceID:            strings.TrimSpace(traceID),
		UserID:             userID,
		ConversationID:     strings.TrimSpace(conversationID),
		UserMessage:        strings.TrimSpace(userMessage),
		RequestNow:         now,
		RequestNowText:     now.In(loadLocation()).Format(datetimeLayout),
		PlanMax:            defaultPlanMax,
		PerTaskBudget:      defaultPerTaskBudget,
		ExecuteMax:         defaultExecuteMax,
		ReplanMax:          defaultReplanMax,
		CompositeRetryMax:  defaultCompositeRetry,
		RepairReserve:      defaultRepairReserve,
		MaxRounds:          defaultExecuteMax + defaultRepairReserve,
		ActionLogs:         make([]string, 0, 32),
		ObservationHistory: make([]ReactRoundObservation, 0, 24),
		TaskActionUsed:     make(map[int]int),
		SeenSlotQueries:    make(map[string]struct{}),
		OriginOrderMap:     make(map[int]int),
		CompositeToolCalled: map[string]bool{
			"SpreadEven":       false,
			"MinContextSwitch": false,
		},
		CompositeToolSuccess: map[string]bool{
			"SpreadEven":       false,
			"MinContextSwitch": false,
		},
		CurrentPlan: PlannerPlan{
			Summary: "initialized, waiting for planner output",
		},
		SlicePlan: RefineSlicePlan{
			Reason: "灏氭湭鍒囩墖",
		},
	}
	if preview == nil {
		return st
	}

	st.TaskClassIDs = append([]int(nil), preview.TaskClassIDs...)
	st.InitialHybridEntries = cloneHybridEntries(preview.HybridEntries)
	st.HybridEntries = cloneHybridEntries(preview.HybridEntries)
	st.AllocatedItems = cloneTaskClassItems(preview.AllocatedItems)
	st.CandidatePlans = cloneWeekSchedules(preview.CandidatePlans)
	st.OriginOrderMap = buildOriginOrderMap(st.HybridEntries)
	return st
}

func loadLocation() *time.Location {
	return agentshared.ShanghaiLocation()
}

func nowToMinute() time.Time {
	return agentshared.NowToMinute()
}

func cloneHybridEntries(src []model.HybridScheduleEntry) []model.HybridScheduleEntry {
	return agentshared.CloneHybridEntries(src)
}

func cloneTaskClassItems(src []model.TaskClassItem) []model.TaskClassItem {
	return agentshared.CloneTaskClassItems(src)
}

func cloneWeekSchedules(src []model.UserWeekSchedule) []model.UserWeekSchedule {
	return agentshared.CloneWeekSchedules(src)
}

// buildOriginOrderMap 鏋勫缓 suggested 浠诲姟鐨勫垵濮嬮『搴忓熀绾匡紙task_item_id -> rank锛夈€?
func buildOriginOrderMap(entries []model.HybridScheduleEntry) map[int]int {
	orderMap := make(map[int]int)
	if len(entries) == 0 {
		return orderMap
	}
	suggested := make([]model.HybridScheduleEntry, 0, len(entries))
	for _, entry := range entries {
		if isMovableSuggestedTask(entry) {
			suggested = append(suggested, entry)
		}
	}
	sort.SliceStable(suggested, func(i, j int) bool {
		left := suggested[i]
		right := suggested[j]
		if left.Week != right.Week {
			return left.Week < right.Week
		}
		if left.DayOfWeek != right.DayOfWeek {
			return left.DayOfWeek < right.DayOfWeek
		}
		if left.SectionFrom != right.SectionFrom {
			return left.SectionFrom < right.SectionFrom
		}
		if left.SectionTo != right.SectionTo {
			return left.SectionTo < right.SectionTo
		}
		return left.TaskItemID < right.TaskItemID
	})
	for i, entry := range suggested {
		orderMap[entry.TaskItemID] = i + 1
	}
	return orderMap
}

// FinalHardCheckPassed 鍒ゆ柇鈥滄渶缁堢粓瀹♀€濇槸鍚︽暣浣撻€氳繃銆?
//
// 鑱岃矗杈圭晫锛?
// 1. 璐熻矗鑱氬悎 physics/order/intent 涓夌被纭牎楠岀粨鏋滐紝缁欐湇鍔″眰涓庢€荤粨闃舵缁熶竴澶嶇敤锛?
// 2. 涓嶈礋璐ｈЕ鍙戠粓瀹★紝涔熶笉璐熻矗鎺ㄥ淇鍔ㄤ綔锛?
// 3. nil state 瑙嗕负鏈€氳繃锛岄伩鍏嶄笂灞傛妸缂哄け缁撴灉璇垽涓烘垚鍔熴€?
func FinalHardCheckPassed(st *ScheduleRefineState) bool {
	if st == nil {
		return false
	}
	return st.HardCheck.PhysicsPassed && st.HardCheck.OrderPassed && st.HardCheck.IntentPassed
}

func isMovableSuggestedTask(entry model.HybridScheduleEntry) bool {
	if strings.TrimSpace(entry.Status) != "suggested" || entry.TaskItemID <= 0 {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(entry.Type), "course") {
		return false
	}
	return true
}
