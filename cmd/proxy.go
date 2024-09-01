package cmd

import (
	"bytes"
	"cmp"
	"encoding/xml"
	"fmt"
	"github.com/elazarl/goproxy"
	"github.com/elazarl/goproxy/ext/auth"
	"github.com/valyala/fastjson"
	"goBlack/common"
	"goBlack/debrid"
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
	Version string   `xml:"version,attr"`
	Channel Channel  `xml:"channel"`
}

type Channel struct {
	XMLName  xml.Name `xml:"channel"`
	Title    string   `xml:"title"`
	AtomLink AtomLink `xml:"link"`
	Items    []Item   `xml:"item"`
}

type AtomLink struct {
	XMLName xml.Name `xml:"link"`
	Rel     string   `xml:"rel,attr"`
	Type    string   `xml:"type,attr"`
}

type Item struct {
	XMLName         xml.Name        `xml:"item"`
	Title           string          `xml:"title"`
	Description     string          `xml:"description"`
	GUID            string          `xml:"guid"`
	ProwlarrIndexer ProwlarrIndexer `xml:"prowlarrindexer"`
	Comments        string          `xml:"comments"`
	PubDate         string          `xml:"pubDate"`
	Size            int64           `xml:"size"`
	Link            string          `xml:"link"`
	Categories      []string        `xml:"category"`
	Enclosure       Enclosure       `xml:"enclosure"`
	TorznabAttrs    []TorznabAttr   `xml:"torznab:attr"`
}

type ProwlarrIndexer struct {
	ID    string `xml:"id,attr"`
	Type  string `xml:"type,attr"`
	Value string `xml:",chardata"`
}

type Enclosure struct {
	URL    string `xml:"url,attr"`
	Length int64  `xml:"length,attr"`
	Type   string `xml:"type,attr"`
}

type TorznabAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
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
}

func NewProxy(config common.Config, deb debrid.Service, cache *common.Cache) *Proxy {
	cfg := config.Proxy
	port := cmp.Or(os.Getenv("PORT"), cfg.Port, "8181")
	return &Proxy{
		port:       port,
		enabled:    cfg.Enabled,
		debug:      cfg.Debug,
		username:   cfg.Username,
		password:   cfg.Password,
		cachedOnly: cfg.CachedOnly,
		debrid:     deb,
		cache:      cache,
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
			hash := strings.ToLower(getItemHash(item))
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

func getItemHash(item Item) string {
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
		//Get torrent file from http link
		//Takes too long, not worth it
		//magnet, err := common.OpenMagnetHttpURL(magnetLink)
		//if err == nil && magnet != nil && magnet.InfoHash != "" {
		//	log.Printf("Magnet: %s", magnet.InfoHash)
		//}
	}
	return infohash

}

func (p *Proxy) ProcessXMLResponse(resp *http.Response) *http.Response {
	if resp == nil || resp.Body == nil {
		return resp
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading response body:", err)
		return resp
	}
	err = resp.Body.Close()
	if err != nil {
		return nil
	}

	var rss RSS
	err = xml.Unmarshal(body, &rss)
	if err != nil {
		log.Printf("Error unmarshalling XML: %v", err)
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
	log.Printf("Found %d infohashes/magnet links", len(hashes))
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

	log.Printf("Report: %d/%d items are cached", len(newItems), len(rss.Channel.Items))
	rss.Channel.Items = newItems

	// rss.Channel.Items = newItems
	modifiedBody, err := xml.MarshalIndent(rss, "", "  ")
	if err != nil {
		log.Printf("Error marshalling XML: %v", err)
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

func (p *Proxy) Start() {
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
	log.Printf("[*] Starting proxy server on %s\n", portFmt)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("%s", portFmt), proxy))
}
