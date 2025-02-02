package arr

import (
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
)

var repairLogger *zerolog.Logger

func getLogger() *zerolog.Logger {
	if repairLogger == nil {
		l := logger.NewLogger("repair", config.GetConfig().LogLevel, os.Stdout)
		repairLogger = &l
	}
	return repairLogger
}

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
			MovieId []int  `json:"movieIds"`
		}{
			Name:    "MoviesSearch",
			MovieId: []int{id},
		}
	default:
		getLogger().Info().Msgf("Unknown arr type: %s", a.Type)
		return
	}

	resp, err := a.Request(http.MethodPost, "api/v3/command", payload)
	if err != nil {
		getLogger().Info().Msgf("Failed to search missing: %v", err)
		return
	}
	if statusOk := strconv.Itoa(resp.StatusCode)[0] == '2'; !statusOk {
		getLogger().Info().Msgf("Failed to search missing: %s", resp.Status)
		return
	}
}

func (a *Arr) Repair(tmdbId string) error {

	getLogger().Info().Msgf("Starting repair for %s", a.Name)
	media, err := a.GetMedia(tmdbId)
	if err != nil {
		getLogger().Info().Msgf("Failed to get %s media: %v", a.Type, err)
		return err
	}
	getLogger().Info().Msgf("Found %d %s media", len(media), a.Type)

	brokenMedia := a.processMedia(media)
	getLogger().Info().Msgf("Found %d %s broken media files", len(brokenMedia), a.Type)

	// Automatic search for missing files
	getLogger().Info().Msgf("Repair completed for %s", a.Name)
	return nil
}

func (a *Arr) processMedia(media []Content) []Content {
	if len(media) <= 1 {
		var brokenMedia []Content
		for _, m := range media {
			// Check if media is accessible
			if !a.isMediaAccessible(m) {
				getLogger().Debug().Msgf("Skipping media check for %s - parent directory not accessible", m.Title)
				continue
			}
			if a.checkMediaFiles(m) {
				a.SearchMissing(m.Id)
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
				// Check if media is accessible
				// First check if we can access this media's directory
				if !a.isMediaAccessible(m) {
					getLogger().Debug().Msgf("Skipping media check for %s - parent directory not accessible", m.Title)
					continue
				}
				if a.checkMediaFilesParallel(m) {
					a.SearchMissing(m.Id)
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
				getLogger().Debug().Msgf("Checking file: %s", f.Path)
				isBroken := false

				if fileIsSymlinked(f.Path) {
					getLogger().Debug().Msgf("File is symlinked: %s", f.Path)

					target, err := getAbsoluteSymlinkTarget(f.Path)
					if err != nil {
						getLogger().Debug().Msgf("Cannot resolve symlink %s: %v", f.Path, err)
						isBroken = true
					} else {
						targetDir := filepath.Dir(target)

						// Check if we've already verified this directory
						if _, exists := a.verifiedDirs.Load(targetDir); exists {
							getLogger().Debug().Msgf("Skipping check for %s - directory already verified", target)
							isBroken = false
							brokenFiles <- isBroken
							continue
						}

						if !fileIsCorrectSymlink(f.Path) {
							getLogger().Debug().Msgf("File is broken: %s", f.Path)
							isBroken = true
							if err := a.DeleteFile(f.Id); err != nil {
								getLogger().Info().Msgf("Failed to delete file: %s %d: %v", f.Path, f.Id, err)
							}
						} else {
							// If the file is readable, mark its directory as verified
							a.verifiedDirs.Store(targetDir, true)
						}
					}
				} else {
					fileDir := filepath.Dir(f.Path)
					if _, exists := a.verifiedDirs.Load(fileDir); exists {
						getLogger().Debug().Msgf("Skipping check for %s - directory already verified", f.Path)
						isBroken = false
						brokenFiles <- isBroken
						continue
					}

					if !fileIsReadable(f.Path) {
						getLogger().Debug().Msgf("File is broken: %s", f.Path)
						isBroken = true
						if err := a.DeleteFile(f.Id); err != nil {
							getLogger().Info().Msgf("Failed to delete file: %s %d: %v", f.Path, f.Id, err)
						}
					} else {
						a.verifiedDirs.Store(fileDir, true)
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
			target, err := getAbsoluteSymlinkTarget(f.Path)
			if err != nil {
				isBroken = true
				continue
			}

			targetDir := filepath.Dir(target)
			if _, exists := a.verifiedDirs.Load(targetDir); exists {
				continue
			}

			if !fileIsCorrectSymlink(f.Path) {
				isBroken = true
				if err := a.DeleteFile(f.Id); err != nil {
					getLogger().Info().Msgf("Failed to delete file: %s %d: %v", f.Path, f.Id, err)
				}
			} else {
				a.verifiedDirs.Store(targetDir, true)
			}
		} else {
			fileDir := filepath.Dir(f.Path)
			if _, exists := a.verifiedDirs.Load(fileDir); exists {
				continue
			}

			if !fileIsReadable(f.Path) {
				isBroken = true
				if err := a.DeleteFile(f.Id); err != nil {
					getLogger().Info().Msgf("Failed to delete file: %s %d: %v", f.Path, f.Id, err)
				}
			} else {
				a.verifiedDirs.Store(fileDir, true)
			}
		}
	}
	return isBroken
}

func (a *Arr) isMediaAccessible(m Content) bool {
	// We're likely to mount the debrid path.
	// So instead of checking the arr path, we check the original path
	// This is because the arr path is likely to be a symlink
	// And we want to check the actual path where the media is stored
	// This is to avoid false positives

	if len(m.Files) == 0 {
		return false
	}

	// Get the first file to check its target location
	file := m.Files[0].Path

	var targetPath string
	fileInfo, err := os.Lstat(file)
	if err != nil {
		repairLogger.Debug().Msgf("Cannot stat file %s: %v", file, err)
		return false
	}

	if fileInfo.Mode()&os.ModeSymlink != 0 {
		// If it's a symlink, get where it points to
		target, err := os.Readlink(file)
		if err != nil {
			repairLogger.Debug().Msgf("Cannot read symlink %s: %v", file, err)
			return false
		}

		// If the symlink target is relative, make it absolute
		if !filepath.IsAbs(target) {
			dir := filepath.Dir(file)
			target = filepath.Join(dir, target)
		}
		targetPath = target
	} else {
		// If it's a regular file, use its path
		targetPath = file
	}

	mediaDir := filepath.Dir(targetPath) // Gets /remote/storage/Movie
	parentDir := filepath.Dir(mediaDir)  // Gets /remote/storage

	_, err = os.Stat(parentDir)
	if err != nil {
		repairLogger.Debug().Msgf("Parent directory of target not accessible for media %s: %s", m.Title, parentDir)
		return false
	}
	return true
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

	buffer := make([]byte, 1)
	_, err = f.Read(buffer)
	if err != nil {
		return err
	}

	return nil
}

func getAbsoluteSymlinkTarget(file string) (string, error) {
	target, err := os.Readlink(file)
	if err != nil {
		return "", err
	}

	if !filepath.IsAbs(target) {
		dir := filepath.Dir(file)
		target = filepath.Join(dir, target)
	}

	return target, nil
}
