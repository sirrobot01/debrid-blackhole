package service

import (
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/engine"
	"github.com/sirrobot01/debrid-blackhole/pkg/repair"
	"sync"
)

type Service struct {
	Repair *repair.Repair
	Arr    *arr.Storage
	Debrid *engine.Engine
}

var (
	instance *Service
	once     sync.Once
)

func New() *Service {
	once.Do(func() {
		arrs := arr.NewStorage()
		deb := debrid.New()
		instance = &Service{
			Repair: repair.New(deb, arrs),
			Arr:    arrs,
			Debrid: deb,
		}
	})
	return instance
}

// GetService returns the singleton instance
func GetService() *Service {
	if instance == nil {
		instance = New()
	}
	return instance
}

func Update() *Service {
	arrs := arr.NewStorage()
	deb := debrid.New()
	instance = &Service{
		Repair: repair.New(deb, arrs),
		Arr:    arrs,
		Debrid: deb,
	}
	return instance
}

func GetDebrid() *engine.Engine {
	return GetService().Debrid
}
