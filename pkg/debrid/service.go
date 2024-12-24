package debrid

type DebridService struct {
	debrids  []Service
	lastUsed int
}

func (d *DebridService) Get() Service {
	if d.lastUsed == 0 {
		return d.debrids[0]
	}
	return d.debrids[d.lastUsed]
}

func (d *DebridService) GetByName(name string) Service {
	for _, deb := range d.debrids {
		if deb.GetName() == name {
			return deb
		}
	}
	return nil
}
