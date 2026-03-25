package agentsvc

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	agentmodel "github.com/LoveLosita/smartflow/backend/agent2/model"
	agentshared "github.com/LoveLosita/smartflow/backend/agent2/shared"
	"github.com/LoveLosita/smartflow/backend/model"
	"github.com/LoveLosita/smartflow/backend/respond"
)

// saveSchedulePlanPreview 鎶婃帓绋嬬粨鏋滀互缁撴瀯鍖?JSON 蹇収鍐欏叆 Redis銆?
//
// 鑱岃矗杈圭晫锛?
// 1. 璐熻矗鎶?finalState 涓殑 summary + candidate_plans 鏀舵暃涓虹紦瀛?DTO锛?
// 2. 璐熻矗浠モ€滃け璐ヤ笉闃绘柇鑱婂ぉ涓婚摼璺€濈殑绛栫暐鎵ц鍐欏叆锛?
// 3. 涓嶈礋璐?SSE 杩斿洖鍗忚锛屼笉璐熻矗鏁版嵁搴撹惤搴撱€?
func (s *AgentService) saveSchedulePlanPreview(ctx context.Context, userID int, chatID string, finalState *agentmodel.SchedulePlanState) {
	// 1. 鍩虹鍓嶇疆鏍￠獙锛歴tate 涓虹┖鏃剁洿鎺ヨ繑鍥烇紝閬垮厤鍐欏叆鍗婃垚鍝佸揩鐓с€?
	if s == nil || finalState == nil {
		return
	}
	normalizedChatID := strings.TrimSpace(chatID)
	if normalizedChatID == "" {
		return
	}

	// 2. 缁勮缂撳瓨蹇収锛?
	// 2.1 summary 浼樺厛鍙?final summary锛岀┖鍊兼椂浣跨敤缁熶竴鍏滃簳鏂囨锛?
	// 2.2 candidate_plans 鍋氬垏鐗囨嫹璐濓紝閬垮厤鍚庣画寮曠敤鍏变韩瀵艰嚧鎰忓瑕嗙洊锛?
	// 2.3 generated_at 鐢ㄤ簬鍓嶇鍒ゆ柇鈥滃綋鍓嶉瑙堢殑鏂伴矞搴︹€濄€?
	summary := strings.TrimSpace(finalState.FinalSummary)
	if summary == "" {
		summary = "排程流程已完成，但未生成结果摘要。"
	}
	preview := &model.SchedulePlanPreviewCache{
		UserID:         userID,
		ConversationID: normalizedChatID,
		TraceID:        strings.TrimSpace(finalState.TraceID),
		Summary:        summary,
		CandidatePlans: cloneWeekSchedules(finalState.CandidatePlans),
		TaskClassIDs:   append([]int(nil), finalState.TaskClassIDs...),
		HybridEntries:  cloneHybridEntries(finalState.HybridEntries),
		AllocatedItems: cloneTaskClassItems(finalState.AllocatedItems),
		GeneratedAt:    time.Now(),
	}

	// 3. 璋冪敤鐩殑锛氬厛鍐?Redis 棰勮锛屼繚璇佸墠绔煡璇㈡帴鍙ｈ兘蹇€熻鍙栫粨鏋勫寲缁撴灉銆?
	// 3.1 Redis 鏄€滃揩璺緞鈥濓紱澶辫触鍙褰曟棩蹇楋紝涓嶄腑鏂富閾捐矾锛?
	// 3.2 澶辫触鍏滃簳鐢卞悗缁?MySQL 蹇収鎵挎帴銆?
	if s.cacheDAO != nil {
		if err := s.cacheDAO.SetSchedulePlanPreviewToCache(ctx, userID, normalizedChatID, preview); err != nil {
			log.Printf("鍐欏叆鎺掔▼棰勮缂撳瓨澶辫触 chat_id=%s: %v", normalizedChatID, err)
		}
	}

	// 4. 璋冪敤鐩殑锛氬悓姝ュ啓 MySQL 鐘舵€佸揩鐓э紝淇濊瘉 Redis 澶辨晥鍚庝粛鍙繛缁井璋冦€?
	// 4.1 杩欓噷閲囩敤鈥滃悓姝ュ啓搴撯€濊€屼笉鏄?outbox锛氬洜涓轰笅涓€杞井璋冭寮哄疄鏃惰鍙栵紱
	// 4.2 蹇収鍐欏叆澶辫触鍙墦鏃ュ織锛屼笉闃绘柇鏈疆鐢ㄦ埛鍥炲锛岄伩鍏嶄綋楠屾姈鍔紱
	// 4.3 revision 鑷鐢?DAO 鐨?upsert 鍐茬獊鏇存柊璐熻矗銆?
	if s.repo != nil {
		snapshot := buildSchedulePlanSnapshotFromState(userID, normalizedChatID, finalState)
		if err := s.repo.UpsertScheduleStateSnapshot(ctx, snapshot); err != nil {
			log.Printf("鍐欏叆鎺掔▼鐘舵€佸揩鐓уけ璐?chat_id=%s: %v", normalizedChatID, err)
		}
	}
}

// saveSchedulePlanPreviewAgent2 鎶?agent2 鐨?schedule_plan 缁撴灉鍐欏叆 Redis 棰勮涓?MySQL 蹇収銆?
//
// 鑱岃矗杈圭晫锛?
// 1. 璐熻矗鎵挎帴鈥滄柊 agent2 棣栨鎺掔▼閾捐矾鈥濈殑鏈€缁堢姸鎬侊紱
// 2. 璐熻矗娌跨敤鐜版湁棰勮缂撳瓨/鐘舵€佸揩鐓у崗璁紝淇濊瘉鏌ヨ鎺ュ彛涓?refine 璇诲彇閫昏緫涓嶉渶瑕佽窡鐫€閲嶅啓锛?
// 3. 涓嶈礋璐?refine 鐘舵€佽浆鎹紝refine 浠嶇户缁蛋鏃ч摼璺殑 saveSchedulePlanPreview銆?
func (s *AgentService) saveSchedulePlanPreviewAgent2(ctx context.Context, userID int, chatID string, finalState *agentmodel.SchedulePlanState) {
	// 1. 鍩虹鍓嶇疆鏍￠獙锛歴tate 涓虹┖鏃剁洿鎺ヨ繑鍥烇紝閬垮厤鍐欏叆鍗婃垚鍝佸揩鐓с€?
	if s == nil || finalState == nil {
		return
	}
	normalizedChatID := strings.TrimSpace(chatID)
	if normalizedChatID == "" {
		return
	}

	// 2. 缁勮缂撳瓨蹇収銆?
	// 2.1 summary 浼樺厛鍙?final summary锛岀┖鍊兼椂浣跨敤缁熶竴鍏滃簳鏂囨锛?
	// 2.2 candidate_plans / hybrid_entries / allocated_items 缁熶竴娣辨嫹璐濓紝閬垮厤缂撳瓨涓?graph state 鍏辩敤搴曞眰鍒囩墖锛?
	// 2.3 generated_at 鐢ㄤ簬鍓嶇鍒ゆ柇鈥滃綋鍓嶉瑙堟槸鍚︿负鏈€鏂版柟妗堚€濄€?
	summary := strings.TrimSpace(finalState.FinalSummary)
	if summary == "" {
		summary = "排程流程已完成，但未生成结果摘要。"
	}
	preview := &model.SchedulePlanPreviewCache{
		UserID:         userID,
		ConversationID: normalizedChatID,
		TraceID:        strings.TrimSpace(finalState.TraceID),
		Summary:        summary,
		CandidatePlans: cloneWeekSchedules(finalState.CandidatePlans),
		TaskClassIDs:   append([]int(nil), finalState.TaskClassIDs...),
		HybridEntries:  cloneHybridEntries(finalState.HybridEntries),
		AllocatedItems: cloneTaskClassItems(finalState.AllocatedItems),
		GeneratedAt:    time.Now(),
	}

	// 3. 鍏堝啓 Redis 棰勮锛屼繚璇佸墠绔煡璇㈡帴鍙ｈ兘绔嬪嵆璇诲彇缁撴瀯鍖栫粨鏋溿€?
	// 3.1 Redis 鏄€滃揩璺緞鈥濓紱
	// 3.2 澶辫触鍙褰曟棩蹇楋紝涓嶄腑鏂亰澶╀富閾捐矾銆?
	if s.cacheDAO != nil {
		if err := s.cacheDAO.SetSchedulePlanPreviewToCache(ctx, userID, normalizedChatID, preview); err != nil {
			log.Printf("鍐欏叆鎺掔▼棰勮缂撳瓨澶辫触 chat_id=%s: %v", normalizedChatID, err)
		}
	}

	// 4. 鍚屾鍐?MySQL 蹇収锛屼繚璇?Redis 澶辨晥鍚庝粛鑳芥仮澶嶉瑙堜笌杩炵画寰皟涓婁笅鏂囥€?
	// 4.1 杩欓噷缁х画淇濇寔鈥滃悓姝ュ啓搴撯€濈瓥鐣ワ紝鍥犱负涓嬩竴杞井璋冨蹇収璇诲彇鏄己瀹炴椂渚濊禆锛?
	// 4.2 鍐欏簱澶辫触鍙墦鏃ュ織锛屼笉闃绘柇鏈疆缁欑敤鎴风殑鏂囨湰鍥炲銆?
	if s.repo != nil {
		snapshot := buildSchedulePlanSnapshotFromAgent2State(userID, normalizedChatID, finalState)
		if err := s.repo.UpsertScheduleStateSnapshot(ctx, snapshot); err != nil {
			log.Printf("鍐欏叆鎺掔▼鐘舵€佸揩鐓уけ璐?chat_id=%s: %v", normalizedChatID, err)
		}
	}
}

// GetSchedulePlanPreview 鎸?conversation_id 璇诲彇缁撴瀯鍖栨帓绋嬮瑙堛€?
//
// 鑱岃矗杈圭晫锛?
// 1. 璐熻矗鍙傛暟褰掍竴鍖栥€佺紦瀛樿鍙栦笌浼氳瘽褰掑睘鏍￠獙锛?
// 2. 璐熻矗鎶婄紦瀛?DTO 杞垚 API 鍝嶅簲 DTO锛?
// 3. 涓嶈礋璐ｈЕ鍙戞帓绋嬶紝涓嶈礋璐ｈˉ绠楃紦瀛樸€?
func (s *AgentService) GetSchedulePlanPreview(ctx context.Context, userID int, chatID string) (*model.GetSchedulePlanPreviewResponse, error) {
	// 1. 鍙傛暟鏍￠獙锛歝onversation_id 涓虹┖鐩存帴杩斿洖鍙傛暟閿欒锛岄伩鍏嶆棤鏁?Redis 璇锋眰銆?
	normalizedChatID := strings.TrimSpace(chatID)
	if normalizedChatID == "" {
		return nil, respond.MissingParam
	}
	if s == nil {
		return nil, errors.New("agent service is not initialized")
	}

	// 2. 鏌ヨ缂撳瓨骞舵牎楠屽綊灞烇細
	// 2.1 缂撳瓨鏈懡涓細缁熶竴杩斿洖鈥滈瑙堜笉瀛樺湪/宸茶繃鏈熲€濓紱
	// 2.2 鍛戒腑浣?user_id 涓嶄竴鑷达細鎸夋湭鍛戒腑澶勭悊锛岄伩鍏嶆硠闇蹭粬浜轰細璇濅俊鎭紱
	// 2.3 澶辫触鍏滃簳锛氱紦瀛樿寮傚父鐩存帴涓婃姏锛岀敱 API 灞傜粺涓€閿欒澶勭悊銆?
	if s.cacheDAO != nil {
		preview, err := s.cacheDAO.GetSchedulePlanPreviewFromCache(ctx, userID, normalizedChatID)
		if err != nil {
			return nil, err
		}
		if preview != nil {
			if preview.UserID > 0 && preview.UserID != userID {
				return nil, respond.SchedulePlanPreviewNotFound
			}
			plans := cloneWeekSchedules(preview.CandidatePlans)
			if plans == nil {
				plans = make([]model.UserWeekSchedule, 0)
			}
			return &model.GetSchedulePlanPreviewResponse{
				ConversationID: normalizedChatID,
				TraceID:        strings.TrimSpace(preview.TraceID),
				Summary:        strings.TrimSpace(preview.Summary),
				CandidatePlans: plans,
				GeneratedAt:    preview.GeneratedAt,
			}, nil
		}
	}

	// 3. Redis 鏈懡涓椂鍥炶惤 MySQL 蹇収锛?
	// 3.1 璇诲彇鎴愬姛鍚庣洿鎺ヨ繑鍥烇紝閬垮厤鐢ㄦ埛鐪嬪埌鈥滈瑙堜笉瀛樺湪鈥濈殑鍋囬槾鎬э紱
	// 3.2 鑻ユ湰娆″懡涓?DB 涓旂紦瀛樺彲鐢紝鍒欓『鎵嬪洖濉?Redis锛屾彁鍗囧悗缁懡涓巼锛?
	// 3.3 DB 涔熸湭鍛戒腑鏃跺啀杩斿洖 not found銆?
	if s.repo != nil {
		snapshot, err := s.repo.GetScheduleStateSnapshot(ctx, userID, normalizedChatID)
		if err != nil {
			return nil, err
		}
		if snapshot != nil {
			response := snapshotToSchedulePlanPreviewResponse(snapshot)
			if s.cacheDAO != nil {
				cachePreview := snapshotToSchedulePlanPreviewCache(snapshot)
				if setErr := s.cacheDAO.SetSchedulePlanPreviewToCache(ctx, userID, normalizedChatID, cachePreview); setErr != nil {
					log.Printf("鍥炲～鎺掔▼棰勮缂撳瓨澶辫触 chat_id=%s: %v", normalizedChatID, setErr)
				}
			}
			return response, nil
		}
	}
	return nil, respond.SchedulePlanPreviewNotFound
}

// cloneWeekSchedules 瀵瑰懆瑙嗗浘鎺掔▼缁撴灉鍋氭繁鎷疯礉锛岄伩鍏嶅垏鐗囧紩鐢ㄥ叡浜€?
func cloneWeekSchedules(src []model.UserWeekSchedule) []model.UserWeekSchedule {
	return agentshared.CloneWeekSchedules(src)
}

// cloneHybridEntries 娣辨嫹璐濇贩鍚堟潯鐩垏鐗囷紝閬垮厤缂撳瓨/鐘舵€佷箣闂寸浉浜掓薄鏌撱€?
func cloneHybridEntries(src []model.HybridScheduleEntry) []model.HybridScheduleEntry {
	return agentshared.CloneHybridEntries(src)
}

// cloneTaskClassItems 娣辨嫹璐濅换鍔″潡鍒囩墖锛堝寘鍚寚閽堝瓧娈碉級锛岄伩鍏嶈法璇锋眰寮曠敤鍏变韩銆?
func cloneTaskClassItems(src []model.TaskClassItem) []model.TaskClassItem {
	return agentshared.CloneTaskClassItems(src)
}

// buildSchedulePlanSnapshotFromState 鎶?graph 杩愯缁撴灉鏄犲皠鎴愬彲鎸佷箙鍖栧揩鐓?DTO銆?
//
// 鑱岃矗杈圭晫锛?
// 1. 璐熻矗瀛楁鏄犲皠涓庢繁鎷疯礉锛岄伩鍏嶈法灞傚叡浜彲鍙樺垏鐗囷紱
// 2. 璐熻矗琛ラ綈 state_version 榛樿鍊硷紱
// 3. 涓嶈礋璐ｆ暟鎹簱鍐欏叆锛堝啓鍏ョ敱 DAO 鎵挎媴锛夈€?
func buildSchedulePlanSnapshotFromState(userID int, conversationID string, st *agentmodel.SchedulePlanState) *model.SchedulePlanStateSnapshot {
	if st == nil {
		return nil
	}
	return &model.SchedulePlanStateSnapshot{
		UserID:           userID,
		ConversationID:   conversationID,
		StateVersion:     model.SchedulePlanStateVersionV1,
		TaskClassIDs:     append([]int(nil), st.TaskClassIDs...),
		Constraints:      append([]string(nil), st.Constraints...),
		HybridEntries:    cloneHybridEntries(st.HybridEntries),
		AllocatedItems:   cloneTaskClassItems(st.AllocatedItems),
		CandidatePlans:   cloneWeekSchedules(st.CandidatePlans),
		UserIntent:       strings.TrimSpace(st.UserIntent),
		Strategy:         strings.TrimSpace(st.Strategy),
		AdjustmentScope:  strings.TrimSpace(st.AdjustmentScope),
		RestartRequested: st.RestartRequested,
		FinalSummary:     strings.TrimSpace(st.FinalSummary),
		Completed:        st.Completed,
		TraceID:          strings.TrimSpace(st.TraceID),
	}
}

// buildSchedulePlanSnapshotFromAgent2State 鎶?agent2 鐨勬帓绋嬬姸鎬佹槧灏勬垚鍙寔涔呭寲蹇収 DTO銆?
//
// 璋冪敤鐩殑锛?
// 1. 杩欒疆鍙縼绉?schedule_plan锛屼笉鍔?refine锛?
// 2. 鍥犳 preview/蹇収鍗忚缁х画澶嶇敤鑰佺粨鏋勶紝浣嗚琛ヤ竴涓€渁gent2 state -> snapshot DTO鈥濈殑鏄犲皠灞傦紱
// 3. 杩欐牱鍙互鍋氬埌锛氳鍒掑垱寤洪摼璺垏鍒?agent2锛岃€?refine / 棰勮鏌ヨ閾捐矾鏆傛椂鏃犻渶澶ф敼銆?
func buildSchedulePlanSnapshotFromAgent2State(userID int, conversationID string, st *agentmodel.SchedulePlanState) *model.SchedulePlanStateSnapshot {
	if st == nil {
		return nil
	}
	return &model.SchedulePlanStateSnapshot{
		UserID:           userID,
		ConversationID:   conversationID,
		StateVersion:     model.SchedulePlanStateVersionV1,
		TaskClassIDs:     append([]int(nil), st.TaskClassIDs...),
		Constraints:      append([]string(nil), st.Constraints...),
		HybridEntries:    cloneHybridEntries(st.HybridEntries),
		AllocatedItems:   cloneTaskClassItems(st.AllocatedItems),
		CandidatePlans:   cloneWeekSchedules(st.CandidatePlans),
		UserIntent:       strings.TrimSpace(st.UserIntent),
		Strategy:         strings.TrimSpace(st.Strategy),
		AdjustmentScope:  strings.TrimSpace(st.AdjustmentScope),
		RestartRequested: st.RestartRequested,
		FinalSummary:     strings.TrimSpace(st.FinalSummary),
		Completed:        st.Completed,
		TraceID:          strings.TrimSpace(st.TraceID),
	}
}

// snapshotToSchedulePlanPreviewCache 鎶?MySQL 蹇収杞崲涓?Redis 棰勮缂撳瓨缁撴瀯銆?
func snapshotToSchedulePlanPreviewCache(snapshot *model.SchedulePlanStateSnapshot) *model.SchedulePlanPreviewCache {
	if snapshot == nil {
		return nil
	}
	summary := strings.TrimSpace(snapshot.FinalSummary)
	if summary == "" {
		summary = "排程流程已完成，但未生成结果摘要。"
	}
	generatedAt := snapshot.UpdatedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	return &model.SchedulePlanPreviewCache{
		UserID:         snapshot.UserID,
		ConversationID: snapshot.ConversationID,
		TraceID:        strings.TrimSpace(snapshot.TraceID),
		Summary:        summary,
		CandidatePlans: cloneWeekSchedules(snapshot.CandidatePlans),
		TaskClassIDs:   append([]int(nil), snapshot.TaskClassIDs...),
		HybridEntries:  cloneHybridEntries(snapshot.HybridEntries),
		AllocatedItems: cloneTaskClassItems(snapshot.AllocatedItems),
		GeneratedAt:    generatedAt,
	}
}

// snapshotToSchedulePlanPreviewResponse 鎶?MySQL 蹇収杞崲涓烘煡璇㈡帴鍙ｅ搷搴斻€?
func snapshotToSchedulePlanPreviewResponse(snapshot *model.SchedulePlanStateSnapshot) *model.GetSchedulePlanPreviewResponse {
	if snapshot == nil {
		return nil
	}
	plans := cloneWeekSchedules(snapshot.CandidatePlans)
	if plans == nil {
		plans = make([]model.UserWeekSchedule, 0)
	}
	summary := strings.TrimSpace(snapshot.FinalSummary)
	if summary == "" {
		summary = "排程流程已完成，但未生成结果摘要。"
	}
	generatedAt := snapshot.UpdatedAt
	if generatedAt.IsZero() {
		generatedAt = time.Now()
	}
	return &model.GetSchedulePlanPreviewResponse{
		ConversationID: snapshot.ConversationID,
		TraceID:        strings.TrimSpace(snapshot.TraceID),
		Summary:        summary,
		CandidatePlans: plans,
		GeneratedAt:    generatedAt,
	}
}
