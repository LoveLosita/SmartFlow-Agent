package agenttools

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/LoveLosita/smartflow/backend/services/runtime/model"
)

// TaskClassUpsertInput 描述任务类写库工具的标准化入参。
//
// 职责边界：
// 1. ID=0 表示创建，ID>0 表示更新；
// 2. Request 直接复用现有 UserAddTaskClassRequest 语义，避免多套字段定义漂移；
// 3. Source 用于记录字段来源（chat/memory/web），不参与业务校验。
type TaskClassUpsertInput struct {
	ID      int
	Request model.UserAddTaskClassRequest
	Source  string
}

// TaskClassUpsertPersistResult 描述任务类写入持久层后的结果。
type TaskClassUpsertPersistResult struct {
	TaskClassID int
	Created     bool
}

// TaskClassWriteDeps 描述任务类写库工具依赖。
//
// 职责边界：
// 1. 工具层只负责参数标准化与结果包装，不直接依赖 DAO；
// 2. UpsertTaskClass 由启动层注入，便于后续替换为 service/DAO 统一实现。
type TaskClassWriteDeps struct {
	UpsertTaskClass func(userID int, input TaskClassUpsertInput) (TaskClassUpsertPersistResult, error)
}

type taskClassValidationResult struct {
	OK     bool     `json:"ok"`
	Issues []string `json:"issues"`
}

type taskClassUpsertToolResult struct {
	Tool        string                    `json:"tool"`
	Success     bool                      `json:"success"`
	TaskClassID int                       `json:"task_class_id,omitempty"`
	Created     bool                      `json:"created,omitempty"`
	Validation  taskClassValidationResult `json:"validation"`
	Error       string                    `json:"error"`
	ErrorCode   string                    `json:"error_code"`
}

func parseTaskClassUpsertInput(args map[string]any) (TaskClassUpsertInput, error) {
	id := 0
	if rawID, exists := args["id"]; exists {
		parsedID, ok := readUpsertInt(rawID)
		if !ok {
			return TaskClassUpsertInput{}, fmt.Errorf("id 参数类型非法，必须为整数")
		}
		id = parsedID
	}

	taskClassRaw, ok := args["task_class"]
	if !ok {
		return TaskClassUpsertInput{}, fmt.Errorf("缺少必填参数 task_class")
	}
	taskClassMap, ok := taskClassRaw.(map[string]any)
	if !ok {
		return TaskClassUpsertInput{}, fmt.Errorf("task_class 参数类型非法，必须是对象")
	}

	// 允许顶层 items 覆盖 task_class.items，便于 LLM 在生成参数时拆分表达。
	if rawItems, exists := args["items"]; exists {
		taskClassMap["items"] = rawItems
	}
	normalizeTaskClassPayload(taskClassMap)

	rawJSON, err := json.Marshal(taskClassMap)
	if err != nil {
		return TaskClassUpsertInput{}, fmt.Errorf("task_class 参数序列化失败")
	}

	var request model.UserAddTaskClassRequest
	if err := json.Unmarshal(rawJSON, &request); err != nil {
		return TaskClassUpsertInput{}, fmt.Errorf("task_class 参数解析失败：%v", err)
	}

	source := ""
	if rawSource, exists := args["source"]; exists {
		if text, ok := rawSource.(string); ok {
			source = strings.TrimSpace(text)
		}
	}
	normalizeTaskClassSemanticRequest(&request)

	return TaskClassUpsertInput{
		ID:      id,
		Request: request,
		Source:  source,
	}, nil
}

// normalizeTaskClassPayload 对 LLM 常见“近义字段/错层字段”做轻量兼容归一化。
//
// 职责边界：
// 1. 负责把已知等价字段映射到后端真实契约，减少“明明填了却校验失败”的误伤；
// 2. 不负责兜底补齐业务必填项（如 mode/config），这些仍由校验层决定是否报错；
// 3. 仅处理本工具已观测到的高频偏差，避免过度“自动纠错”掩盖真实输入问题。
func normalizeTaskClassPayload(taskClassMap map[string]any) {
	if len(taskClassMap) == 0 {
		return
	}

	// 1. 兼容日期字段错层：
	// 1.1 若顶层 start_date/end_date 缺失；
	// 1.2 且 config.start_date/config.end_date 有值；
	// 1.3 则抬升到顶层，匹配 UserAddTaskClassRequest 契约。
	configMap, _ := readAnyMap(taskClassMap["config"])
	promoteStringField(taskClassMap, configMap, "start_date")
	promoteStringField(taskClassMap, configMap, "end_date")

	// 2. 兼容 items 的语义字段：
	// 2.1 content 缺失时，尝试从 description/title/name 回填；
	// 2.2 order 缺失或非法时，按当前顺序补 1..N；
	// 2.3 失败时不抛错，留给校验层输出明确问题。
	normalizeTaskClassItems(taskClassMap)
}

func promoteStringField(top map[string]any, config map[string]any, key string) {
	if top == nil {
		return
	}
	if strings.TrimSpace(readAnyString(top[key])) != "" {
		return
	}
	if strings.TrimSpace(readAnyString(config[key])) == "" {
		return
	}
	top[key] = strings.TrimSpace(readAnyString(config[key]))
}

func normalizeTaskClassItems(taskClassMap map[string]any) {
	rawItems, exists := taskClassMap["items"]
	if !exists {
		return
	}
	itemList, ok := rawItems.([]any)
	if !ok {
		return
	}
	for idx := range itemList {
		itemMap, ok := itemList[idx].(map[string]any)
		if !ok {
			continue
		}

		if !hasPositiveInt(itemMap["order"]) {
			itemMap["order"] = idx + 1
		}
		if strings.TrimSpace(readAnyString(itemMap["content"])) == "" {
			content := firstNonEmptyString(
				readAnyString(itemMap["content"]),
				readAnyString(itemMap["description"]),
				readAnyString(itemMap["title"]),
				readAnyString(itemMap["name"]),
			)
			if strings.TrimSpace(content) != "" {
				itemMap["content"] = strings.TrimSpace(content)
			}
		}
		itemList[idx] = itemMap
	}
	taskClassMap["items"] = itemList
}

func readAnyMap(raw any) (map[string]any, bool) {
	if raw == nil {
		return nil, false
	}
	value, ok := raw.(map[string]any)
	return value, ok
}

func readAnyString(raw any) string {
	switch value := raw.(type) {
	case string:
		return value
	default:
		return ""
	}
}

// normalizeTaskClassSemanticRequest 归一化任务类语义画像字段。
//
// 职责边界：
// 1. 负责把 LLM 或用户可能给出的中文/近义值收口成稳定枚举；
// 2. 不负责补默认值，字段缺失仍由上层决定是否接受；
// 3. 归一化失败时保留原值，交给校验层输出明确错误。
func normalizeTaskClassSemanticRequest(req *model.UserAddTaskClassRequest) {
	if req == nil {
		return
	}
	if normalized := normalizeSubjectType(req.SubjectType); normalized != "" {
		req.SubjectType = normalized
	}
	if normalized := normalizeLevelValue(req.DifficultyLevel); normalized != "" {
		req.DifficultyLevel = normalized
	}
	if normalized := normalizeLevelValue(req.CognitiveIntensity); normalized != "" {
		req.CognitiveIntensity = normalized
	}
}

func normalizeSubjectType(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	switch value {
	case "quantitative", "计算型", "计算", "理工", "理工型":
		return "quantitative"
	case "memory", "记忆型", "记忆", "背诵型", "背诵":
		return "memory"
	case "reading", "阅读型", "阅读":
		return "reading"
	case "mixed", "混合型", "混合":
		return "mixed"
	default:
		return ""
	}
}

func normalizeLevelValue(raw string) string {
	value := strings.TrimSpace(strings.ToLower(raw))
	switch value {
	case "low", "低":
		return "low"
	case "medium", "中", "中等":
		return "medium"
	case "high", "高":
		return "high"
	default:
		return ""
	}
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func hasPositiveInt(raw any) bool {
	switch value := raw.(type) {
	case int:
		return value > 0
	case int8:
		return value > 0
	case int16:
		return value > 0
	case int32:
		return value > 0
	case int64:
		return value > 0
	case float32:
		return int(value) > 0
	case float64:
		return int(value) > 0
	default:
		return false
	}
}

func validateTaskClassUpsertRequest(req model.UserAddTaskClassRequest, id int) []string {
	issues := make([]string, 0)
	if id < 0 {
		issues = append(issues, "id 不能小于 0")
	}
	if strings.TrimSpace(req.Name) == "" {
		issues = append(issues, "name 不能为空")
	}
	mode := strings.TrimSpace(strings.ToLower(req.Mode))
	if mode != "auto" && mode != "manual" {
		issues = append(issues, "mode 仅支持 auto/manual")
	}
	if mode == "auto" {
		if strings.TrimSpace(req.StartDate) == "" || strings.TrimSpace(req.EndDate) == "" {
			issues = append(issues, "auto 模式必须提供 start_date/end_date")
		} else {
			startDate, err1 := time.ParseInLocation("2006-01-02", strings.TrimSpace(req.StartDate), time.Local)
			endDate, err2 := time.ParseInLocation("2006-01-02", strings.TrimSpace(req.EndDate), time.Local)
			if err1 != nil || err2 != nil {
				issues = append(issues, "start_date/end_date 日期格式非法，需为 YYYY-MM-DD")
			} else if startDate.After(endDate) {
				issues = append(issues, "start_date 不能晚于 end_date")
			}
		}
	}
	if strings.TrimSpace(req.SubjectType) != "" && normalizeSubjectType(req.SubjectType) == "" {
		issues = append(issues, "subject_type 仅支持 quantitative/memory/reading/mixed")
	}
	if strings.TrimSpace(req.DifficultyLevel) != "" && normalizeLevelValue(req.DifficultyLevel) == "" {
		issues = append(issues, "difficulty_level 仅支持 low/medium/high")
	}
	if strings.TrimSpace(req.CognitiveIntensity) != "" && normalizeLevelValue(req.CognitiveIntensity) == "" {
		issues = append(issues, "cognitive_intensity 仅支持 low/medium/high")
	}
	if strings.TrimSpace(req.SubjectType) == "" {
		issues = append(issues, "subject_type 不能为空")
	}
	if strings.TrimSpace(req.DifficultyLevel) == "" {
		issues = append(issues, "difficulty_level 不能为空")
	}
	if strings.TrimSpace(req.CognitiveIntensity) == "" {
		issues = append(issues, "cognitive_intensity 不能为空")
	}
	if req.Config.TotalSlots <= 0 {
		issues = append(issues, "config.total_slots 必须大于 0")
	}
	strategy := strings.TrimSpace(strings.ToLower(req.Config.Strategy))
	if strategy != "steady" && strategy != "rapid" {
		issues = append(issues, "config.strategy 仅支持 steady/rapid")
	}
	for _, section := range req.Config.ExcludedSlots {
		// 1. excluded_slots 在粗排算法中按“半天块索引”解释，而不是原子节次；
		// 2. 每个块固定映射 2 节：1->1-2，2->3-4，...，6->11-12；
		// 3. 若放行 7~12，会在 buildTimeGrid 扩展时生成 13~24 节，触发数组越界。
		if section < 1 || section > 6 {
			issues = append(issues, "config.excluded_slots 仅允许 1~6（半天块索引，每块=2节）")
			break
		}
	}
	for _, dayOfWeek := range req.Config.ExcludedDaysOfWeek {
		// 1. excluded_days_of_week 属于“整天不可排”的硬约束；
		// 2. 仅允许 1~7，对应周一到周日；
		// 3. 非法值会导致粗排过滤口径失真，因此统一在写入口拦截。
		if dayOfWeek < 1 || dayOfWeek > 7 {
			issues = append(issues, "config.excluded_days_of_week 仅允许 1~7（周一到周日）")
			break
		}
	}
	if len(req.Items) == 0 {
		issues = append(issues, "items 不能为空")
	}
	for index, item := range req.Items {
		if item.Order <= 0 {
			issues = append(issues, fmt.Sprintf("items[%d].order 必须大于 0", index))
		}
		if strings.TrimSpace(item.Content) == "" {
			issues = append(issues, fmt.Sprintf("items[%d].content 不能为空", index))
		}
	}
	return uniqueTaskClassIssues(issues)
}

func uniqueTaskClassIssues(issues []string) []string {
	if len(issues) == 0 {
		return issues
	}
	seen := make(map[string]struct{}, len(issues))
	out := make([]string, 0, len(issues))
	for _, issue := range issues {
		trimmed := strings.TrimSpace(issue)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func readUpsertUserID(raw any) (int, bool) {
	return readUpsertInt(raw)
}

func readUpsertInt(raw any) (int, bool) {
	switch value := raw.(type) {
	case int:
		return value, true
	case int8:
		return int(value), true
	case int16:
		return int(value), true
	case int32:
		return int(value), true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case float32:
		return int(value), true
	default:
		return 0, false
	}
}

func marshalTaskClassUpsertResult(result taskClassUpsertToolResult) string {
	raw, err := json.Marshal(result)
	if err != nil {
		return `{"tool":"upsert_task_class","success":false,"error":"result encode failed","error_code":"encode_failed"}`
	}
	return string(raw)
}
