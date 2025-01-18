package arr

import (
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/common"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
)

var (
	repairLogger zerolog.Logger = common.NewLogger("repair", "info", os.Stdout)
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
		repairLogger.Info().Msgf("Unknown arr type: %s", a.Type)
		return
	}

	resp, err := a.Request(http.MethodPost, "api/v3/command", payload)
	if err != nil {
		repairLogger.Info().Msgf("Failed to search missing: %v", err)
		return
	}
	if statusOk := strconv.Itoa(resp.StatusCode)[0] == '2'; !statusOk {
		repairLogger.Info().Msgf("Failed to search missing: %s", resp.Status)
		return
	}
}

func (a *Arr) Repair(tmdbId string) error {

	repairLogger.Info().Msgf("Starting repair for %s", a.Name)
	media, err := a.GetMedia(tmdbId)
	if err != nil {
		repairLogger.Info().Msgf("Failed to get %s media: %v", a.Type, err)
		return err
	}
	repairLogger.Info().Msgf("Found %d %s media", len(media), a.Type)

	brokenMedia := a.processMedia(media)
	repairLogger.Info().Msgf("Found %d %s broken media files", len(brokenMedia), a.Type)

	// Automatic search for missing files
	for _, m := range brokenMedia {
		repairLogger.Debug().Msgf("Searching missing for %s", m.Title)
		a.SearchMissing(m.Id)
	}
	repairLogger.Info().Msgf("Search missing completed for %s", a.Name)
	repairLogger.Info().Msgf("Repair completed for %s", a.Name)
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
				repairLogger.Debug().Msgf("Checking file: %s", f.Path)
				isBroken := false
				if fileIsSymlinked(f.Path) {
					repairLogger.Debug().Msgf("File is symlinked: %s", f.Path)
					if !fileIsCorrectSymlink(f.Path) {
						repairLogger.Debug().Msgf("File is broken: %s", f.Path)
						isBroken = true
						if err := a.DeleteFile(f.Id); err != nil {
							repairLogger.Info().Msgf("Failed to delete file: %s %d: %v", f.Path, f.Id, err)
						}
					}
				} else {
					repairLogger.Debug().Msgf("File is not symlinked: %s", f.Path)
					if !fileIsReadable(f.Path) {
						repairLogger.Debug().Msgf("File is broken: %s", f.Path)
						isBroken = true
						if err := a.DeleteFile(f.Id); err != nil {
							repairLogger.Info().Msgf("Failed to delete file: %s %d: %v", f.Path, f.Id, err)
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
					repairLogger.Info().Msgf("Failed to delete file: %s %d: %v", f.Path, f.Id, err)
				}
			}
		} else {
			if !fileIsReadable(f.Path) {
				isBroken = true
				if err := a.DeleteFile(f.Id); err != nil {
					repairLogger.Info().Msgf("Failed to delete file: %s %d: %v", f.Path, f.Id, err)
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
