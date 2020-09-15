package mobile

import (
	"github.com/b582q9/go-textile-block-immutable/core"
	"github.com/b582q9/go-textile-block-immutable/mill"
	"github.com/b582q9/go-textile-block-immutable/pb"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
)

// AddSchema adds a new schema via schema mill
func (m *Mobile) AddSchema(node []byte) ([]byte, error) {
	if !m.node.Started() {
		return nil, core.ErrStopped
	}

	model := new(pb.Node)
	if err := proto.Unmarshal(node, model); err != nil {
		return nil, err
	}
	marshaler := jsonpb.Marshaler{
		OrigName: true,
	}
	jsn, err := marshaler.MarshalToString(model)
	if err != nil {
		return nil, err
	}

	added, err := m.node.AddFileIndex(&mill.Schema{}, core.AddFileConfig{
		Input: []byte(jsn),
		Media: "application/json",
	})
	if err != nil {
		return nil, err
	}

	m.node.FlushCafes()

	return proto.Marshal(added)
}
