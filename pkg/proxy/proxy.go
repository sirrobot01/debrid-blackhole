package proxy

import (
	"bytes"
	"cmp"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"github.com/elazarl/goproxy"
	"github.com/elazarl/goproxy/ext/auth"
	"github.com/valyala/fastjson"
	"goBlack/common"
	"goBlack/pkg/debrid"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
)

type RSS struct {
	XMLName xml.Name `xml:"rss"`
	Text    string   `xml:",chardata"`
	Version string   `xml:"version,attr"`
	Atom    string   `xml:"atom,attr"`
	Torznab string   `xml:"torznab,attr"`
	Channel struct {
		Text string `xml:",chardata"`
		Link struct {
			Text string `xml:",chardata"`
			Rel  string `xml:"rel,attr"`
			Type string `xml:"type,attr"`
		} `xml:"link"`
		Title string `xml:"title"`
		Items []Item `xml:"item"`
	} `xml:"channel"`
}

type Item struct {
	Text            string `xml:",chardata"`
	Title           string `xml:"title"`
	Description     string `xml:"description"`
	GUID            string `xml:"guid"`
	ProwlarrIndexer struct {
		Text string `xml:",chardata"`
		ID   string `xml:"id,attr"`
		Type string `xml:"type,attr"`
	} `xml:"prowlarrindexer"`
	Comments  string   `xml:"comments"`
	PubDate   string   `xml:"pubDate"`
	Size      string   `xml:"size"`
	Link      string   `xml:"link"`
	Category  []string `xml:"category"`
	Enclosure struct {
		Text   string `xml:",chardata"`
		URL    string `xml:"url,attr"`
		Length string `xml:"length,attr"`
		Type   string `xml:"type,attr"`
	} `xml:"enclosure"`
	TorznabAttrs []struct {
		Text  string `xml:",chardata"`
		Name  string `xml:"name,attr"`
		Value string `xml:"value,attr"`
	} `xml:"attr"`
}

type Proxy struct {
	port       string
	enabled    bool
	debug      bool
	username   string
	password   string
	cachedOnly bool
	debrid     debrid.Service
	cache      *common.Cache
	logger     *log.Logger
}

func NewProxy(config common.Config, deb *debrid.DebridService, cache *common.Cache) *Proxy {
	cfg := config.Proxy
	port := cmp.Or(os.Getenv("PORT"), cfg.Port, "8181")
	return &Proxy{
		port:       port,
		enabled:    cfg.Enabled,
		debug:      cfg.Debug,
		username:   cfg.Username,
		password:   cfg.Password,
		cachedOnly: *cfg.CachedOnly,
		debrid:     deb.Get(),
		cache:      cache,
		logger:     common.NewLogger("Proxy", os.Stdout),
	}
}

func (p *Proxy) ProcessJSONResponse(resp *http.Response) *http.Response {
	if resp == nil || resp.Body == nil {
		return resp
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp
	}
	err = resp.Body.Close()
	if err != nil {
		return nil
	}

	var par fastjson.Parser
	v, err := par.ParseBytes(body)
	if err != nil {
		// If it's not JSON, return the original response
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp
	}

	// Modify the JSON

	// Serialize the modified JSON back to bytes
	modifiedBody := v.MarshalTo(nil)

	// Set the modified body back to the response
	resp.Body = io.NopCloser(bytes.NewReader(modifiedBody))
	resp.ContentLength = int64(len(modifiedBody))
	resp.Header.Set("Content-Length", string(rune(len(modifiedBody))))

	return resp

}

func (p *Proxy) ProcessResponse(resp *http.Response) *http.Response {
	if resp == nil || resp.Body == nil {
		return resp
	}
	contentType := resp.Header.Get("Content-Type")
	switch contentType {
	case "application/json":
		return resp // p.ProcessJSONResponse(resp)
	case "application/xml":
		return p.ProcessXMLResponse(resp)
	case "application/rss+xml":
		return p.ProcessXMLResponse(resp)
	default:
		return resp
	}
}

func getItemsHash(items []Item) map[string]string {

	var wg sync.WaitGroup
	idHashMap := sync.Map{} // Use sync.Map for concurrent access

	for _, item := range items {
		wg.Add(1)
		go func(item Item) {
			defer wg.Done()
			hash := strings.ToLower(item.getHash())
			if hash != "" {
				idHashMap.Store(item.GUID, hash) // Store directly into sync.Map
			}
		}(item)
	}
	wg.Wait()

	// Convert sync.Map to regular map
	finalMap := make(map[string]string)
	idHashMap.Range(func(key, value interface{}) bool {
		finalMap[key.(string)] = value.(string)
		return true
	})

	return finalMap
}

func (item Item) getHash() string {
	infohash := ""

	for _, attr := range item.TorznabAttrs {
		if attr.Name == "infohash" {
			return attr.Value
		}
	}

	if strings.Contains(item.GUID, "magnet:?") {
		magnet, err := common.GetMagnetInfo(item.GUID)
		if err == nil && magnet != nil && magnet.InfoHash != "" {
			return magnet.InfoHash
		}
	}

	magnetLink := item.Link

	if magnetLink == "" {
		// We can't check the availability of the torrent without a magnet link or infohash
		return ""
	}

	if strings.Contains(magnetLink, "magnet:?") {
		magnet, err := common.GetMagnetInfo(magnetLink)
		if err == nil && magnet != nil && magnet.InfoHash != "" {
			return magnet.InfoHash
		}
	}

	//Check Description for infohash
	hash := common.ExtractInfoHash(item.Description)
	if hash == "" {
		// Check Title for infohash
		hash = common.ExtractInfoHash(item.Comments)
	}
	infohash = hash
	if infohash == "" {
		if strings.Contains(magnetLink, "http") {
			h, _ := common.GetInfohashFromURL(magnetLink)
			if h != "" {
				infohash = h
			}
		}
	}
	return infohash

}

func (p *Proxy) ProcessXMLResponse(resp *http.Response) *http.Response {
	if resp == nil || resp.Body == nil {
		return resp
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		p.logger.Println("Error reading response body:", err)
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp
	}
	err = resp.Body.Close()
	if err != nil {
		return nil
	}

	var rss RSS
	err = xml.Unmarshal(body, &rss)
	if err != nil {
		p.logger.Printf("Error unmarshalling XML: %v", err)
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp
	}
	indexer := ""
	if len(rss.Channel.Items) > 0 {
		indexer = rss.Channel.Items[0].ProwlarrIndexer.Text
	} else {
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp
	}

	// Step 4: Extract infohash or magnet URI, manipulate data
	IdsHashMap := getItemsHash(rss.Channel.Items)
	hashes := make([]string, 0)
	for _, hash := range IdsHashMap {
		if hash != "" {
			hashes = append(hashes, hash)
		}
	}
	availableHashesMap := p.debrid.IsAvailable(hashes)
	newItems := make([]Item, 0, len(rss.Channel.Items))

	if len(hashes) > 0 {
		for _, item := range rss.Channel.Items {
			hash := IdsHashMap[item.GUID]
			if hash == "" {
				continue
			}
			isCached, exists := availableHashesMap[hash]
			if !exists || !isCached {
				continue
			}
			newItems = append(newItems, item)
		}
	}

	if len(newItems) > 0 {
		p.logger.Printf("[%s Report]: %d/%d items are cached || Found %d infohash", indexer, len(newItems), len(rss.Channel.Items), len(hashes))
	} else {
		// This will prevent the indexer from being disabled by the arr
		p.logger.Printf("[%s Report]: No Items are cached; Return only first item with [UnCached]", indexer)
		item := rss.Channel.Items[0]
		item.Title = fmt.Sprintf("%s [UnCached]", item.Title)
		newItems = append(newItems, item)
	}

	rss.Channel.Items = newItems
	modifiedBody, err := xml.MarshalIndent(rss, "", "  ")
	if err != nil {
		p.logger.Printf("Error marshalling XML: %v", err)
		resp.Body = io.NopCloser(bytes.NewReader(body))
		return resp
	}
	modifiedBody = append([]byte(xml.Header), modifiedBody...)

	// Set the modified body back to the response
	resp.Body = io.NopCloser(bytes.NewReader(modifiedBody))
	return resp
}

func UrlMatches(re *regexp.Regexp) goproxy.ReqConditionFunc {
	return func(req *http.Request, ctx *goproxy.ProxyCtx) bool {
		return re.MatchString(req.URL.String())
	}
}

func (p *Proxy) Start(ctx context.Context) error {
	username, password := p.username, p.password
	proxy := goproxy.NewProxyHttpServer()
	if username != "" || password != "" {
		// Set up basic auth for proxy
		auth.ProxyBasic(proxy, "my_realm", func(user, pwd string) bool {
			return user == username && password == pwd
		})
	}

	proxy.OnRequest(goproxy.ReqHostMatches(regexp.MustCompile("^.443$"))).HandleConnect(goproxy.AlwaysMitm)
	proxy.OnResponse(
		UrlMatches(regexp.MustCompile("^.*/api\\?t=(search|tvsearch|movie)(&.*)?$")),
		goproxy.StatusCodeIs(http.StatusOK, http.StatusAccepted)).DoFunc(
		func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
			return p.ProcessResponse(resp)
		})

	proxy.Verbose = p.debug
	portFmt := fmt.Sprintf(":%s", p.port)
	srv := &http.Server{
		Addr:    portFmt,
		Handler: proxy,
	}
	p.logger.Printf("[*] Starting proxy server on %s\n", portFmt)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			p.logger.Printf("Error starting proxy server: %v\n", err)
		}
	}()
	<-ctx.Done()
	p.logger.Println("Shutting down gracefully...")
	return srv.Shutdown(context.Background())
}
