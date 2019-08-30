package position

import (
	"sync"
)

type memPos struct {
	mutex *sync.RWMutex
	pos map[string]int64
}

func NewMemPos() Interface {
	return &memPos{
		mutex: &sync.RWMutex{},
		pos: make(map[string]int64),
	}
}

func (m *memPos) GetOffset(devIno string) int64 {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.pos[devIno]
}

func (m *memPos) SetOffset(devIno string, offset int64) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.pos[devIno] = offset
}

