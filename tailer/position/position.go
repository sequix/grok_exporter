package position

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/pkg/errors"
)

const positionFileMode = 0600

type Interface interface {
	GetOffset(devIno string) int64
	SetOffset(devIno string, offset int64)
}

// 记录日志读取的offset
type position struct {
	mutex        *sync.RWMutex
	logger       logrus.FieldLogger
	path         string
	interval     time.Duration
	offsets      map[string]int64 // ino -> offset
	done         chan struct{}
}

func New(log logrus.FieldLogger, positionFilePath string, syncInterval time.Duration) (Interface, error) {
	buf, err := ioutil.ReadFile(positionFilePath)
	if err != nil {
		if !os.IsNotExist(err) {

			return nil, errors.Wrap(err, "position read file failed")
		}
	}

	if len(buf) == 0 {
		buf = []byte("{}")
	}

	offsets := make(map[string]int64)
	if err := json.Unmarshal(buf, &offsets); err != nil {
		return nil, errors.Wrap(err, "position unmarshal failed")
	}

	p := &position{
		mutex:    &sync.RWMutex{},
		logger:   log.WithField("component", "position"),
		path:     positionFilePath,
		interval: syncInterval,
		offsets:  offsets,
	}
	go p.run()
	return p, nil
}

func (p *position) run() {
	tick := time.NewTimer(p.interval)
	defer tick.Stop()
	for {
		tick.Reset(p.interval)
		select {
		case <-tick.C:
			p.sync()
		case <-p.done:
			return
		}
	}
}

func (p *position) stop() {
	close(p.done)
}

func (p *position) sync() {
	f, err := os.OpenFile(p.path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, positionFileMode)
	if err != nil {
		p.logger.Error("open position file " + p.path)
		return
	}
	defer f.Close()

	p.mutex.RLock()

	buf, err := json.Marshal(p.offsets)
	if err != nil {
		p.logger.Error("marshal position failed")
		return
	}

	p.mutex.RUnlock()

	if _, err := f.Write(buf); err != nil {
		p.logger.Error("write position failed")
		return
	}
}

func (p *position) GetOffset(devIno string) int64 {
	p.mutex.RLock()
	defer p.mutex.RUnlock()
	offset, ok := p.offsets[devIno]
	if !ok {
		return 0
	}
	return offset
}

func (p *position) SetOffset(devIno string, offset int64) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.offsets[devIno] = offset
}