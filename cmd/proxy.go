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

type SafeItems struct {
	mu    sync.Mutex
	Items []Item
}

func (s *SafeItems) Add(item Item) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Items = append(s.Items, item)
}

func (s *SafeItems) Get() []Item {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Items
}

func ProcessJSONResponse(resp *http.Response, deb debrid.Service) *http.Response {
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

	var p fastjson.Parser
	v, err := p.ParseBytes(body)
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

func ProcessResponse(resp *http.Response, deb debrid.Service) *http.Response {
	if resp == nil || resp.Body == nil {
		return resp
	}
	contentType := resp.Header.Get("Content-Type")
	switch contentType {
	case "application/json":
		return ProcessJSONResponse(resp, deb)
	case "application/xml":
		return ProcessXMLResponse(resp, deb)
	case "application/rss+xml":
		return ProcessXMLResponse(resp, deb)
	default:
		return resp
	}
}

func XMLItemIsCached(item Item, deb debrid.Service) bool {
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
		return false
	}
	var magnet *common.Magnet
	var err error

	if infohash == "" {
		magnet, err = common.GetMagnetInfo(magnetLink)
		if err != nil {
			log.Println("Error getting magnet info:", err)
			return false
		}
	} else {
		magnet = &common.Magnet{
			InfoHash: infohash,
			Name:     item.Title,
			Link:     magnetLink,
		}
	}
	if magnet == nil {
		log.Println("Error getting magnet info")
		return false
	}
	return deb.IsAvailable(magnet)

}

func ProcessXMLResponse(resp *http.Response, deb debrid.Service) *http.Response {
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
		log.Fatalf("Error unmarshalling XML: %v", err)
		return resp
	}
	newItems := &SafeItems{}
	var wg sync.WaitGroup

	// Step 4: Extract infohash or magnet URI, manipulate data
	for _, item := range rss.Channel.Items {
		wg.Add(1)
		go func(item Item) {
			defer wg.Done()
			if XMLItemIsCached(item, deb) {
				newItems.Add(item)
			}
		}(item)
	}
	wg.Wait()
	rss.Channel.Items = newItems.Get()

	// rss.Channel.Items = newItems
	modifiedBody, err := xml.MarshalIndent(rss, "", "  ")
	if err != nil {
		log.Printf("Error marshalling XML: %v", err)
		return resp
	}
	modifiedBody = append([]byte(xml.Header), modifiedBody...)

	if err != nil {
		log.Fatalf("Error marshalling XML: %v", err)
		return resp
	}

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

func StartProxy(config *common.Config, deb debrid.Service) {
	username, password := config.Proxy.Username, config.Proxy.Password
	cfg := config.Proxy
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
			return ProcessResponse(resp, deb)
		})

	port := cmp.Or(cfg.Port, "8181")
	proxy.Verbose = cfg.Debug
	port = fmt.Sprintf(":%s", port)
	log.Printf("Starting proxy server on %s\n", port)
	log.Fatal(http.ListenAndServe(fmt.Sprintf("%s", port), proxy))
}
