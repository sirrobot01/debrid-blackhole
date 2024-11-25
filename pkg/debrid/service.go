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
