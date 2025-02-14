package engine

type Engine struct {
	Debrids  []Service
	LastUsed int
}

func (d *Engine) Get() Service {
	if d.LastUsed == 0 {
		return d.Debrids[0]
	}
	return d.Debrids[d.LastUsed]
}

func (d *Engine) GetByName(name string) Service {
	for _, deb := range d.Debrids {
		if deb.GetName() == name {
			return deb
		}
	}
	return nil
}

func (d *Engine) GetDebrids() []Service {
	return d.Debrids
}
