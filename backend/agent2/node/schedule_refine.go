package agentnode

import (
	"context"

	agentmodel "github.com/LoveLosita/smartflow/backend/agent2/model"
	agentrefine "github.com/LoveLosita/smartflow/backend/agent2/node/schedule_refine_impl"
	"github.com/LoveLosita/smartflow/backend/model"
)

// ScheduleRefineState is the node-layer alias for refine state.
type ScheduleRefineState = agentrefine.ScheduleRefineState

// ScheduleRefineGraphRunInput is the node-layer alias for refine graph input.
type ScheduleRefineGraphRunInput = agentrefine.ScheduleRefineGraphRunInput

// NewScheduleRefineState creates refine state from the previous preview snapshot.
func NewScheduleRefineState(traceID string, userID int, conversationID string, userMessage string, preview *model.SchedulePlanPreviewCache) *ScheduleRefineState {
	return agentrefine.NewScheduleRefineState(traceID, userID, conversationID, userMessage, preview)
}

// FinalHardCheckPassed reports whether the final refine hard check passed.
func FinalHardCheckPassed(st *ScheduleRefineState) bool {
	return agentrefine.FinalHardCheckPassed(st)
}

// ScheduleRefineNodes is a temporary compatibility facade.
// The real refine implementation still lives in schedule_refine_impl until the next split round lands.
type ScheduleRefineNodes struct {
	input ScheduleRefineGraphRunInput
}

// NewScheduleRefineNodes stores the refine graph input.
func NewScheduleRefineNodes(input ScheduleRefineGraphRunInput) (*ScheduleRefineNodes, error) {
	return &ScheduleRefineNodes{input: input}, nil
}

func (n *ScheduleRefineNodes) Contract(ctx context.Context, st *agentmodel.ScheduleRefineState) (*agentmodel.ScheduleRefineState, error) {
	return st, nil
}

func (n *ScheduleRefineNodes) Plan(ctx context.Context, st *agentmodel.ScheduleRefineState) (*agentmodel.ScheduleRefineState, error) {
	return st, nil
}

func (n *ScheduleRefineNodes) Slice(ctx context.Context, st *agentmodel.ScheduleRefineState) (*agentmodel.ScheduleRefineState, error) {
	return st, nil
}

func (n *ScheduleRefineNodes) Route(ctx context.Context, st *agentmodel.ScheduleRefineState) (*agentmodel.ScheduleRefineState, error) {
	return st, nil
}

func (n *ScheduleRefineNodes) React(ctx context.Context, st *agentmodel.ScheduleRefineState) (*agentmodel.ScheduleRefineState, error) {
	return st, nil
}

func (n *ScheduleRefineNodes) HardCheck(ctx context.Context, st *agentmodel.ScheduleRefineState) (*agentmodel.ScheduleRefineState, error) {
	return st, nil
}

func (n *ScheduleRefineNodes) Summary(ctx context.Context, st *agentmodel.ScheduleRefineState) (*agentmodel.ScheduleRefineState, error) {
	return st, nil
}

// RunScheduleRefineGraph is kept as the single executable entry for refine.
func RunScheduleRefineGraph(ctx context.Context, input ScheduleRefineGraphRunInput) (*ScheduleRefineState, error) {
	return agentrefine.RunScheduleRefineGraph(ctx, input)
}
