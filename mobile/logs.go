package mobile

import (
	"github.com/b582q9/go-textile-block-immutable/pb"
	"github.com/golang/protobuf/proto"
)

// SetLogLevel calls core SetLogLevel
func (m *Mobile) SetLogLevel(level []byte) error {
	mlevel := new(pb.LogLevel)
	if err := proto.Unmarshal(level, mlevel); err != nil {
		return err
	}

	return m.node.SetLogLevel(mlevel, false)
}
