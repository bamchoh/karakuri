package adapter

import (
	"karakuri/internal/domain/protocol"
	"karakuri/internal/domain/variable"
)

type variableStoreAccessorImpl struct {
	vs *variable.VariableStore
}

// NewVariableStoreAccessor は VariableStore を protocol.VariableStoreAccessor にラップする
func NewVariableStoreAccessor(vs *variable.VariableStore) protocol.VariableStoreAccessor {
	return &variableStoreAccessorImpl{vs: vs}
}

func (a *variableStoreAccessorImpl) GetEnabledNodePublishings(protocolType string) []protocol.NodePublishingInfo {
	publishings := a.vs.GetAllNodePublishings(protocolType)
	result := make([]protocol.NodePublishingInfo, 0, len(publishings))
	for varID, np := range publishings {
		if !np.Enabled {
			continue
		}
		v, err := a.vs.GetVariable(varID)
		if err != nil {
			continue
		}
		result = append(result, protocol.NodePublishingInfo{
			VariableID:   varID,
			VariableName: v.Name,
			DataType:     string(v.DataType),
			AccessMode:   np.AccessMode,
		})
	}
	return result
}

func (a *variableStoreAccessorImpl) ReadVariableValue(variableID string) (interface{}, error) {
	v, err := a.vs.GetVariable(variableID)
	if err != nil {
		return nil, err
	}
	return v.Value, nil
}

func (a *variableStoreAccessorImpl) WriteVariableValue(variableID string, value interface{}) error {
	return a.vs.UpdateValue(variableID, value)
}

func (a *variableStoreAccessorImpl) WriteVariableFieldValue(variableID, fieldPath string, value interface{}) error {
	return a.vs.UpdateFieldValue(variableID, fieldPath, value)
}

func (a *variableStoreAccessorImpl) GetStructFields(typeName string) []protocol.StructFieldInfo {
	st, err := a.vs.GetStructType(typeName)
	if err != nil {
		return nil
	}
	result := make([]protocol.StructFieldInfo, len(st.Fields))
	for i, f := range st.Fields {
		result[i] = protocol.StructFieldInfo{
			Name:     f.Name,
			DataType: string(f.DataType),
		}
	}
	return result
}
