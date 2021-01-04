package mobile

import (
	"github.com/b582q9/go-textile-sapien/core"
	"github.com/golang/protobuf/proto"
)

// AddMessage adds a message to a thread
func (m *Mobile) AddMessage(threadId string, body string) (string, error) {
	if !m.node.Started() {
		return "", core.ErrStopped
	}

	thrd := m.node.Thread(threadId)
	if thrd == nil {
		return "", core.ErrThreadNotFound
	}

	hash, err := thrd.AddMessage("", body)
	if err != nil {
		return "", err
	}

	m.node.FlushCafes()

	return hash.B58String(), nil
}

// Messages calls core Messages
func (m *Mobile) Messages(offset string, limit int, threadId string) ([]byte, error) {
	if !m.node.Started() {
		return nil, core.ErrStopped
	}

	msgs, err := m.node.Messages(offset, limit, threadId)
	if err != nil {
		return nil, err
	}

	return proto.Marshal(msgs)
}
