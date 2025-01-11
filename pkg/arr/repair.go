package arr

import (
	"goBlack/common"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
)

var (
	repairLogger *log.Logger = common.NewLogger("Repair", os.Stdout)
)

func (a *Arr) SearchMissing(id int) {
	var payload interface{}

	switch a.Type {
	case Sonarr:
		payload = struct {
			Name     string `json:"name"`
			SeriesId int    `json:"seriesId"`
		}{
			Name:     "SeriesSearch",
			SeriesId: id,
		}
	case Radarr:
		payload = struct {
			Name    string `json:"name"`
			MovieId int    `json:"movieId"`
		}{
			Name:    "MoviesSearch",
			MovieId: id,
		}
	default:
		repairLogger.Printf("Unknown arr type: %s\n", a.Type)
		return
	}

	resp, err := a.Request(http.MethodPost, "api/v3/command", payload)
	if err != nil {
		repairLogger.Printf("Failed to search missing: %v\n", err)
		return
	}
	if statusOk := strconv.Itoa(resp.StatusCode)[0] == '2'; !statusOk {
		repairLogger.Printf("Failed to search missing: %s\n", resp.Status)
		return
	}
}

func (a *Arr) Repair(tmdbId string) error {

	repairLogger.Printf("Starting repair for %s\n", a.Name)
	media, err := a.GetMedia(tmdbId)
	if err != nil {
		repairLogger.Printf("Failed to get %s media: %v\n", a.Type, err)
		return err
	}
	repairLogger.Printf("Found %d %s media\n", len(media), a.Type)

	brokenMedia := a.processMedia(media)
	repairLogger.Printf("Found %d %s broken media files\n", len(brokenMedia), a.Type)

	// Automatic search for missing files
	for _, m := range brokenMedia {
		a.SearchMissing(m.Id)
	}
	repairLogger.Printf("Search missing completed for %s\n", a.Name)
	repairLogger.Printf("Repair completed for %s\n", a.Name)
	return nil
}

func (a *Arr) processMedia(media []Content) []Content {
	if len(media) <= 1 {
		var brokenMedia []Content
		for _, m := range media {
			if a.checkMediaFiles(m) {
				brokenMedia = append(brokenMedia, m)
			}
		}
		return brokenMedia
	}

	workerCount := runtime.NumCPU() * 4
	if len(media) < workerCount {
		workerCount = len(media)
	}

	jobs := make(chan Content)
	results := make(chan Content)
	var brokenMedia []Content

	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for m := range jobs {
				if a.checkMediaFilesParallel(m) {
					results <- m
				}
			}
		}()
	}

	go func() {
		for _, m := range media {
			jobs <- m
		}
		close(jobs)
	}()

	go func() {
		wg.Wait()
		close(results)
	}()

	for m := range results {
		brokenMedia = append(brokenMedia, m)
	}

	return brokenMedia
}

func (a *Arr) checkMediaFilesParallel(m Content) bool {
	if len(m.Files) <= 1 {
		return a.checkMediaFiles(m)
	}

	fileWorkers := runtime.NumCPU() * 2
	if len(m.Files) < fileWorkers {
		fileWorkers = len(m.Files)
	}

	fileJobs := make(chan contentFile)
	brokenFiles := make(chan bool, len(m.Files))

	var fileWg sync.WaitGroup
	for i := 0; i < fileWorkers; i++ {
		fileWg.Add(1)
		go func() {
			defer fileWg.Done()
			for f := range fileJobs {
				isBroken := false
				if fileIsSymlinked(f.Path) {
					if !fileIsCorrectSymlink(f.Path) {
						isBroken = true
						if err := a.DeleteFile(f.Id); err != nil {
							repairLogger.Printf("Failed to delete file: %s %d: %v\n", f.Path, f.Id, err)
						}
					}
				} else {
					if !fileIsReadable(f.Path) {
						isBroken = true
						if err := a.DeleteFile(f.Id); err != nil {
							repairLogger.Printf("Failed to delete file: %s %d: %v\n", f.Path, f.Id, err)
						}
					}
				}
				brokenFiles <- isBroken
			}
		}()
	}

	go func() {
		for _, f := range m.Files {
			fileJobs <- f
		}
		close(fileJobs)
	}()

	go func() {
		fileWg.Wait()
		close(brokenFiles)
	}()

	isBroken := false
	for broken := range brokenFiles {
		if broken {
			isBroken = true
		}
	}

	return isBroken
}

func (a *Arr) checkMediaFiles(m Content) bool {
	isBroken := false
	for _, f := range m.Files {
		if fileIsSymlinked(f.Path) {
			if !fileIsCorrectSymlink(f.Path) {
				isBroken = true
				if err := a.DeleteFile(f.Id); err != nil {
					repairLogger.Printf("Failed to delete file: %s %d: %v\n", f.Path, f.Id, err)
				}
			}
		} else {
			if !fileIsReadable(f.Path) {
				isBroken = true
				if err := a.DeleteFile(f.Id); err != nil {
					repairLogger.Printf("Failed to delete file: %s %d: %v\n", f.Path, f.Id, err)
				}
			}
		}
	}
	return isBroken
}

func fileIsSymlinked(file string) bool {
	info, err := os.Lstat(file)
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeSymlink != 0
}

func fileIsCorrectSymlink(file string) bool {
	target, err := os.Readlink(file)
	if err != nil {
		return false
	}

	if !filepath.IsAbs(target) {
		dir := filepath.Dir(file)
		target = filepath.Join(dir, target)
	}

	return fileIsReadable(target)
}

func fileIsReadable(filePath string) bool {
	// First check if file exists and is accessible
	info, err := os.Stat(filePath)
	if err != nil {
		return false
	}

	// Check if it's a regular file
	if !info.Mode().IsRegular() {
		return false
	}

	// Try to read the first 1024 bytes
	err = checkFileStart(filePath)
	if err != nil {
		return false
	}

	return true
}

func checkFileStart(filePath string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	buffer := make([]byte, 1024)
	_, err = io.ReadAtLeast(f, buffer, 1024)
	if err != nil && err != io.EOF {
		return err
	}

	return nil
}
