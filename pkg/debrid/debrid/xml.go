package debrid

import (
	"fmt"
	"github.com/beevik/etree"
	"github.com/sirrobot01/decypharr/internal/request"
	"net/http"
	"os"
	path "path/filepath"
	"time"
)

// resetPropfindResponse resets the propfind response cache for the specified parent directories.
func (c *Cache) resetPropfindResponse() error {
	// Right now, parents are hardcoded
	parents := []string{"__all__", "torrents"}
	// Reset only the parent directories
	// Convert the parents to a keys
	// This is a bit hacky, but it works
	// Instead of deleting all the keys, we only delete the parent keys, e.g __all__/ or torrents/
	keys := make([]string, 0, len(parents))
	for _, p := range parents {
		// Construct the key
		// construct url
		url := path.Clean(path.Join("/webdav", c.client.GetName(), p))
		key0 := fmt.Sprintf("propfind:%s:0", url)
		key1 := fmt.Sprintf("propfind:%s:1", url)
		keys = append(keys, key0, key1)
	}

	// Delete the keys
	for _, k := range keys {
		c.PropfindResp.Delete(k)
	}
	c.logger.Trace().Msgf("Reset XML cache for %s", c.client.GetName())
	return nil
}

func (c *Cache) RefreshParentXml() error {
	parents := []string{"__all__", "torrents"}
	torrents := c.GetListing()
	clientName := c.client.GetName()
	for _, parent := range parents {
		if err := c.refreshParentXml(torrents, clientName, parent); err != nil {
			return fmt.Errorf("failed to refresh XML for %s: %v", parent, err)
		}
	}
	c.logger.Trace().Msgf("Refreshed XML cache for %s", c.client.GetName())
	return nil
}

func (c *Cache) refreshParentXml(torrents []os.FileInfo, clientName, parent string) error {
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
	baseUrl := path.Clean(fmt.Sprintf("/webdav/%s/%s", clientName, parent))
	parentPath := fmt.Sprintf("%s/", baseUrl)
	addDirectoryResponse(multistatus, parentPath, parent, currentTime)

	// Add torrents to the XML
	for _, torrent := range torrents {
		name := torrent.Name()
		// Note the path structure change - parent first, then torrent name
		torrentPath := fmt.Sprintf("/webdav/%s/%s/%s/",
			clientName,
			parent,
			name,
		)

		addDirectoryResponse(multistatus, torrentPath, name, currentTime)
	}

	// Convert to XML string
	xmlData, err := doc.WriteToBytes()
	if err != nil {
		return fmt.Errorf("failed to generate XML: %v", err)
	}

	// Store in cache
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

	// Add href - ensure it's properly formatted
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

	// Add content type for directories
	contentTypeElem := propElem.CreateElement("D:getcontenttype")
	contentTypeElem.SetText("httpd/unix-directory")

	// Add length (size) - directories typically have zero size
	contentLengthElem := propElem.CreateElement("D:getcontentlength")
	contentLengthElem.SetText("0")

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
