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
	Port       string `json:"port"`
	Enabled    bool   `json:"enabled"`
	Debug      bool   `json:"debug"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	CachedOnly bool   `json:"cached_only"`
	Debrid     debrid.Service
}

func NewProxy(config common.Config, deb debrid.Service) *Proxy {
	cfg := config.Proxy
	port := cmp.Or(os.Getenv("PORT"), cfg.Port, "8181")
	return &Proxy{
		Port:       port,
		Enabled:    cfg.Enabled,
		Debug:      cfg.Debug,
		Username:   cfg.Username,
		Password:   cfg.Password,
		CachedOnly: cfg.CachedOnly,
		Debrid:     deb,
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
	IdHashMap := make(map[string]string)
	for _, item := range items {
		hash := getItemHash(item)
		IdHashMap[item.GUID] = hash
	}
	return IdHashMap
}

func getItemHash(item Item) string {
	magnetLink := ""
	infohash := ""

	// Extract magnet link from the link or comments
	if strings.Contains(item.Link, "magnet:?") {
		magnetLink = item.Link
	} else if strings.Contains(item.GUID, "magnet:?") {
		magnetLink = item.GUID
	}

	// Extract infohash from <torznab:attr> elements
	for _, attr := range item.TorznabAttrs {
		if attr.Name == "infohash" {
			infohash = attr.Value
		}
	}
	if magnetLink == "" && infohash == "" {
		// We can't check the availability of the torrent without a magnet link or infohash
		return ""
	}
	var magnet *common.Magnet
	var err error

	if infohash == "" {
		magnet, err = common.GetMagnetInfo(magnetLink)
		if err != nil || magnet == nil || magnet.InfoHash == "" {
			log.Println("Error getting magnet info:", err)
			return ""
		}
		infohash = magnet.InfoHash
	}
	return infohash

}

func (p *Proxy) ProcessXMLResponse(resp *http.Response) *http.Response {
	if resp == nil || resp.Body == nil {
		return resp
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		if p.Debug {
			log.Println("Error reading response body:", err)
		}
		return resp
	}
	err = resp.Body.Close()
	if err != nil {
		return nil
	}

	var rss RSS
	err = xml.Unmarshal(body, &rss)
	if err != nil {
		if p.Debug {
			log.Printf("Error unmarshalling XML: %v", err)
		}
		return resp
	}
	newItems := make([]Item, 0)

	// Step 4: Extract infohash or magnet URI, manipulate data
	IdsHashMap := getItemsHash(rss.Channel.Items)
	hashes := make([]string, 0)
	for _, hash := range IdsHashMap {
		if hash != "" {
			hashes = append(hashes, hash)
		}
	}
	if len(hashes) == 0 {
		// No infohashes or magnet links found, should we return the original response?
		return resp
	}
	availableHashes := p.Debrid.IsAvailable(hashes)
	for _, item := range rss.Channel.Items {
		hash := IdsHashMap[item.GUID]
		if hash == "" {
			// newItems = append(newItems, item)
			continue
		}
		isCached, exists := availableHashes[hash]
		if !exists {
			// newItems = append(newItems, item)
			continue
		}
		if !isCached {
			continue
		}
		newItems = append(newItems, item)
	}
	log.Printf("Report: %d/%d items are cached", len(newItems), len(rss.Channel.Items))
	rss.Channel.Items = newItems

	// rss.Channel.Items = newItems
	modifiedBody, err := xml.MarshalIndent(rss, "", "  ")
	if err != nil {
		if p.Debug {
			log.Printf("Error marshalling XML: %v", err)
		}
		return resp
	}
	modifiedBody = append([]byte(xml.Header), modifiedBody...)

	// Set the modified body back to the response
	resp.Body = io.NopCloser(bytes.NewReader(modifiedBody))
	resp.ContentLength = int64(len(modifiedBody))
	resp.Header.Set("Content-Length", string(rune(len(modifiedBody))))

	return resp
}

func UrlMatches(re *regexp.Regexp) goproxy.ReqConditionFunc {
	return func(req *http.Request, ctx *goproxy.ProxyCtx) bool {
		return re.MatchString(req.URL.String())
	}
}

func (p *Proxy) Start() {
	username, password := p.Username, p.Password
	proxy := goproxy.NewProxyHttpServer()
	if username != "" || password != "" {
		// Set up basic auth for proxy
		auth.ProxyBasic(proxy, "my_realm", func(user, pwd string) bool {
			return user == username && password == pwd
		})
	}

	proxy.OnRequest(goproxy.ReqHostMatches(regexp.MustCompile("^.443$"))).HandleConnect(goproxy.AlwaysMitm)
	proxy.OnResponse(UrlMatches(regexp.MustCompile("^.*/api\\?t=(search|tvsearch|movie)(&.*)?$"))).DoFunc(
		func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
			return p.ProcessResponse(resp)
		})

	proxy.Verbose = p.Debug
	portFmt := fmt.Sprintf(":%s", p.Port)
	log.Printf("[*] Starting proxy server on %s\n", portFmt)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("%s", portFmt), proxy))
}
