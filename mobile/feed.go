package mobile

import (
	"github.com/b582q9/go-textile-sapien/core"
	"github.com/b582q9/go-textile-sapien/pb"
	"github.com/golang/protobuf/proto"
)

// Feed calls core Feed
func (m *Mobile) Feed(req []byte) ([]byte, error) {
	if !m.node.Started() {
		return nil, core.ErrStopped
	}

	mreq := new(pb.FeedRequest)
	if err := proto.Unmarshal(req, mreq); err != nil {
		return nil, err
	}

	items, err := m.node.Feed(mreq)
	if err != nil {
		return nil, err
	}

	return proto.Marshal(items)
}
