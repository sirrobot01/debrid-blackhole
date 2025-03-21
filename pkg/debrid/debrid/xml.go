package debrid

import (
	"fmt"
	"github.com/beevik/etree"
	"github.com/sirrobot01/debrid-blackhole/internal/request"
	"net/http"
	"net/url"
	path "path/filepath"
	"time"
)

func (c *Cache) RefreshXml() error {
	parents := []string{"__all__", "torrents"}
	for _, parent := range parents {
		if err := c.refreshParentXml(parent); err != nil {
			return fmt.Errorf("failed to refresh XML for %s: %v", parent, err)
		}
	}

	c.logger.Debug().Msgf("Refreshed XML cache for %s", c.client.GetName())
	return nil
}

func (c *Cache) refreshParentXml(parent string) error {
	// Define the WebDAV namespace
	davNS := "DAV:"

	// Create the root multistatus element
	doc := etree.NewDocument()
	doc.CreateProcInst("xml", `version="1.0" encoding="UTF-8"`)

	multistatus := doc.CreateElement("D:multistatus")
	multistatus.CreateAttr("xmlns:D", davNS)

	// Get the current timestamp in RFC1123 format (WebDAV format)
	currentTime := time.Now().UTC().Format(http.TimeFormat)

	// Add the parent directory
	parentPath := fmt.Sprintf("/webdav/%s/%s/", c.client.GetName(), parent)
	addDirectoryResponse(multistatus, parentPath, parent, currentTime)

	// Add torrents to the XML
	torrents := c.GetListing()
	for _, torrent := range torrents {
		torrentName := torrent.Name()
		torrentPath := fmt.Sprintf("/webdav/%s/%s/%s/",
			c.client.GetName(),
			url.PathEscape(torrentName),
			parent,
		)

		addDirectoryResponse(multistatus, torrentPath, torrentName, currentTime)
	}

	// Convert to XML string
	xmlData, err := doc.WriteToBytes()
	if err != nil {
		return fmt.Errorf("failed to generate XML: %v", err)
	}

	// Store in cache
	// Construct the keys
	baseUrl := path.Clean(fmt.Sprintf("/webdav/%s/%s", c.client.GetName()))
	key0 := fmt.Sprintf("propfind:%s:0", baseUrl)
	key1 := fmt.Sprintf("propfind:%s:1", baseUrl)

	res := PropfindResponse{
		Data:        xmlData,
		GzippedData: request.Gzip(xmlData),
		Ts:          time.Now(),
	}
	c.PropfindResp.Store(key0, res)
	c.PropfindResp.Store(key1, res)
	return nil
}

func addDirectoryResponse(multistatus *etree.Element, href, displayName, modTime string) *etree.Element {
	responseElem := multistatus.CreateElement("D:response")

	// Add href
	hrefElem := responseElem.CreateElement("D:href")
	hrefElem.SetText(href)

	// Add propstat
	propstatElem := responseElem.CreateElement("D:propstat")

	// Add prop
	propElem := propstatElem.CreateElement("D:prop")

	// Add resource type (collection = directory)
	resourceTypeElem := propElem.CreateElement("D:resourcetype")
	resourceTypeElem.CreateElement("D:collection")

	// Add display name
	displayNameElem := propElem.CreateElement("D:displayname")
	displayNameElem.SetText(displayName)

	// Add last modified time
	lastModElem := propElem.CreateElement("D:getlastmodified")
	lastModElem.SetText(modTime)

	// Add supported lock
	lockElem := propElem.CreateElement("D:supportedlock")
	lockEntryElem := lockElem.CreateElement("D:lockentry")

	lockScopeElem := lockEntryElem.CreateElement("D:lockscope")
	lockScopeElem.CreateElement("D:exclusive")

	lockTypeElem := lockEntryElem.CreateElement("D:locktype")
	lockTypeElem.CreateElement("D:write")

	// Add status
	statusElem := propstatElem.CreateElement("D:status")
	statusElem.SetText("HTTP/1.1 200 OK")

	return responseElem
}
