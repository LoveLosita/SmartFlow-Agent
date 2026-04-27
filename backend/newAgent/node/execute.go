package newagentnode

import (
	"context"

	newagentexecute "github.com/LoveLosita/smartflow/backend/newAgent/node/execute"
)

type ExecuteNodeInput = newagentexecute.ExecuteNodeInput
type ExecuteRoundObservation = newagentexecute.ExecuteRoundObservation

func RunExecuteNode(ctx context.Context, input ExecuteNodeInput) error {
	return newagentexecute.RunExecuteNode(ctx, input)
}
