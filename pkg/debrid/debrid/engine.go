package debrid

import (
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/debrid/types"
)

type Engine struct {
	Clients  map[string]types.Client
	Caches   map[string]*Cache
	LastUsed string
}

func NewEngine() *Engine {
	cfg := config.Get()
	clients := make(map[string]types.Client)

	caches := make(map[string]*Cache)

	for _, dc := range cfg.Debrids {
		client := createDebridClient(dc)
		logger := client.GetLogger()
		if dc.UseWebDav {
			caches[dc.Name] = New(dc, client)
			logger.Info().Msg("Debrid Service started with WebDAV")
		} else {
			logger.Info().Msg("Debrid Service started")
		}
		clients[dc.Name] = client
	}

	d := &Engine{
		Clients:  clients,
		LastUsed: "",
		Caches:   caches,
	}
	return d
}

func (d *Engine) Get() types.Client {
	if d.LastUsed == "" {
		for _, c := range d.Clients {
			return c
		}
	}
	return d.Clients[d.LastUsed]
}

func (d *Engine) GetByName(name string) types.Client {
	return d.Clients[name]
}

func (d *Engine) GetDebrids() map[string]types.Client {
	return d.Clients
}
