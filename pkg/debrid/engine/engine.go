package engine

import (
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/debrid"
)

type Engine struct {
	Debrids  []debrid.Client
	LastUsed int
}

func (d *Engine) Get() debrid.Client {
	if d.LastUsed == 0 {
		return d.Debrids[0]
	}
	return d.Debrids[d.LastUsed]
}

func (d *Engine) GetByName(name string) debrid.Client {
	for _, deb := range d.Debrids {
		if deb.GetName() == name {
			return deb
		}
	}
	return nil
}

func (d *Engine) GetDebrids() []debrid.Client {
	return d.Debrids
}
