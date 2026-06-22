package config

import (
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

func GetOperationMode(mode string) operationmode.OperationMode {
	if mode == "" {
		return operationmode.OperationModeOrganize
	}
	parsed, err := operationmode.ParseOperationMode(mode)
	if err != nil {
		return operationmode.OperationModeOrganize
	}
	return parsed
}

func (o *OutputOperationConfig) GetOperationMode() operationmode.OperationMode {
	return GetOperationMode(string(o.OperationMode))
}

// GetOperationMode delegates to OutputOperationConfig for backward compatibility
// with callers that reference OutputConfig.GetOperationMode().
func (o *OutputConfig) GetOperationMode() operationmode.OperationMode {
	return o.Operation.GetOperationMode()
}
