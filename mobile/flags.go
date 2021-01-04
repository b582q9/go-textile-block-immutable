package mobile

import "github.com/b582q9/go-textile-sapien/core"

// AddFlag adds a flag targeted at the given block
func (m *Mobile) AddFlag(blockId string) (string, error) {
	if !m.node.Started() {
		return "", core.ErrStopped
	}

	block, err := m.node.Block(blockId)
	if err != nil {
		return "", err
	}

	thrd := m.node.Thread(block.Thread)
	if thrd == nil {
		return "", core.ErrThreadNotFound
	}

	hash, err := thrd.AddFlag(block.Id)
	if err != nil {
		return "", err
	}

	m.node.FlushCafes()

	return hash.B58String(), nil
}
