package workflow

import (
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/organizer"
)

// normalizeInPlaceLinkMode keeps the artifact, execution, and history lanes
// aligned. In-place strategies do not support linking; their irrelevant link
// setting must not redirect a staged copy onto the user's real source path.
func normalizeInPlaceLinkMode(cmd ApplyCmd) ApplyCmd {
	if cmd.OperationMode == operationmode.OperationModeInPlace || cmd.OperationMode == operationmode.OperationModeInPlaceNoRenameFolder {
		cmd.Organize.LinkMode = organizer.LinkModeNone
	}
	return cmd
}
