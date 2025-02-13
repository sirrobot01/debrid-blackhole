package repair

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/engine"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Repair struct {
	Jobs       []Job `json:"jobs"`
	arrs       *arr.Storage
	deb        engine.Service
	duration   time.Duration
	runOnStart bool
	ZurgURL    string
	IsZurg     bool
	logger     zerolog.Logger
}

func New(deb *engine.Engine, arrs *arr.Storage) *Repair {
	cfg := config.GetConfig()
	duration, err := parseSchedule(cfg.Repair.Interval)
	if err != nil {
		duration = time.Hour * 24
	}
	r := &Repair{
		arrs:       arrs,
		deb:        deb.Get(),
		logger:     logger.NewLogger("repair", cfg.LogLevel, os.Stdout),
		duration:   duration,
		runOnStart: cfg.Repair.RunOnStart,
		ZurgURL:    cfg.Repair.ZurgURL,
	}
	if r.ZurgURL != "" {
		r.IsZurg = true
	}
	return r
}

type Job struct {
	ID          string     `json:"id"`
	Arrs        []*arr.Arr `json:"arrs"`
	MediaIDs    []string   `json:"media_ids"`
	StartedAt   time.Time  `json:"created_at"`
	CompletedAt time.Time  `json:"finished_at"`
	FailedAt    time.Time  `json:"failed_at"`

	Error string `json:"error"`
}

func (r *Repair) NewJob(arrs []*arr.Arr, mediaIDs []string) *Job {
	return &Job{
		ID:        uuid.New().String(),
		Arrs:      arrs,
		MediaIDs:  mediaIDs,
		StartedAt: time.Now(),
	}
}

func (r *Repair) PreRunChecks() error {
	// Check if zurg url is reachable
	if !r.IsZurg {
		return nil
	}
	resp, err := http.Get(fmt.Sprint(r.ZurgURL, "/http/version.txt"))
	if err != nil {
		r.logger.Debug().Err(err).Msgf("Precheck failed: Failed to reach zurg at %s", r.ZurgURL)
		return err
	}
	if resp.StatusCode != http.StatusOK {
		r.logger.Debug().Msgf("Precheck failed: Zurg returned %d", resp.StatusCode)
		return err
	}
	return nil
}

func (r *Repair) Repair(arrs []*arr.Arr, mediaIds []string) error {

	j := r.NewJob(arrs, mediaIds)

	if err := r.PreRunChecks(); err != nil {
		return err
	}
	var wg sync.WaitGroup
	errors := make(chan error)
	for _, a := range j.Arrs {
		wg.Add(1)
		go func(a *arr.Arr) {
			defer wg.Done()
			if len(j.MediaIDs) == 0 {
				if err := r.RepairArr(a, ""); err != nil {
					log.Printf("Error repairing %s: %v", a.Name, err)
					errors <- err
				}
			} else {
				for _, id := range j.MediaIDs {
					if err := r.RepairArr(a, id); err != nil {
						log.Printf("Error repairing %s: %v", a.Name, err)
						errors <- err
					}
				}
			}
		}(a)
	}
	wg.Wait()
	close(errors)
	err := <-errors
	if err != nil {
		j.FailedAt = time.Now()
		j.Error = err.Error()
		return err
	}
	j.CompletedAt = time.Now()
	return nil
}

func (r *Repair) Start(ctx context.Context) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg := config.GetConfig()

	if r.runOnStart {
		r.logger.Info().Msgf("Running initial repair")
		go func() {
			if err := r.Repair(r.arrs.GetAll(), []string{}); err != nil {
				r.logger.Info().Msgf("Error during initial repair: %v", err)
			}
		}()
	}

	ticker := time.NewTicker(r.duration)
	defer ticker.Stop()

	r.logger.Info().Msgf("Starting repair worker with %v interval", r.duration)

	for {
		select {
		case <-ctx.Done():
			r.logger.Info().Msg("Repair worker stopped")
			return nil
		case t := <-ticker.C:
			r.logger.Info().Msgf("Running repair at %v", t.Format("15:04:05"))
			err := r.Repair(r.arrs.GetAll(), []string{})
			if err != nil {
				r.logger.Info().Msgf("Error during repair: %v", err)
			}

			// If using time-of-day schedule, reset the ticker for next day
			if strings.Contains(cfg.Repair.Interval, ":") {
				ticker.Reset(r.duration)
			}

			r.logger.Info().Msgf("Next scheduled repair at %v", t.Add(r.duration).Format("15:04:05"))
		}
	}
}

func (r *Repair) RepairArr(a *arr.Arr, tmdbId string) error {

	cfg := config.GetConfig()

	r.logger.Info().Msgf("Starting repair for %s", a.Name)
	media, err := a.GetMedia(tmdbId)
	if err != nil {
		r.logger.Info().Msgf("Failed to get %s media: %v", a.Type, err)
		return err
	}
	r.logger.Info().Msgf("Found %d %s media", len(media), a.Type)

	if len(media) == 0 {
		r.logger.Info().Msgf("No %s media found", a.Type)
		return nil
	}
	// Check first media to confirm mounts are accessible
	if !r.isMediaAccessible(media[0]) {
		r.logger.Info().Msgf("Skipping repair. Parent directory not accessible for. Check your mounts")
		return nil
	}

	semaphore := make(chan struct{}, runtime.NumCPU()*4)
	totalBrokenItems := 0
	var wg sync.WaitGroup
	for _, m := range media {
		wg.Add(1)
		semaphore <- struct{}{}
		go func(m arr.Content) {
			defer wg.Done()
			defer func() { <-semaphore }()
			brokenItems := r.getBrokenFiles(m)
			if brokenItems != nil {
				r.logger.Debug().Msgf("Found %d broken files for %s", len(brokenItems), m.Title)
				if !cfg.Repair.SkipDeletion {
					if err := a.DeleteFiles(brokenItems); err != nil {
						r.logger.Info().Msgf("Failed to delete broken items for %s: %v", m.Title, err)
					}
				}
				if err := a.SearchMissing(brokenItems); err != nil {
					r.logger.Info().Msgf("Failed to search missing items for %s: %v", m.Title, err)
				}
				totalBrokenItems += len(brokenItems)
			}
		}(m)
	}
	wg.Wait()
	r.logger.Info().Msgf("Repair completed for %s. %d broken items found", a.Name, totalBrokenItems)
	return nil
}

func (r *Repair) isMediaAccessible(m arr.Content) bool {
	files := m.Files
	if len(files) == 0 {
		return false
	}
	firstFile := files[0]
	r.logger.Debug().Msgf("Checking parent directory for %s", firstFile.Path)
	if _, err := os.Stat(firstFile.Path); os.IsNotExist(err) {
		return false
	}
	// Check symlink parent directory
	symlinkPath := getSymlinkTarget(firstFile.Path)

	r.logger.Debug().Msgf("Checking symlink parent directory for %s", symlinkPath)

	if symlinkPath != "" {
		parentSymlink := filepath.Dir(filepath.Dir(symlinkPath)) // /mnt/zurg/torrents/movie/movie.mkv -> /mnt/zurg/torrents
		if _, err := os.Stat(parentSymlink); os.IsNotExist(err) {
			return false
		}
	}
	return true
}

func (r *Repair) getBrokenFiles(media arr.Content) []arr.ContentFile {

	if r.IsZurg {
		return r.getZurgBrokenFiles(media)
	} else {
		return r.getFileBrokenFiles(media)
	}
}

func (r *Repair) getFileBrokenFiles(media arr.Content) []arr.ContentFile {
	// This checks symlink target, try to get read a tiny bit of the file

	brokenFiles := make([]arr.ContentFile, 0)

	uniqueParents := make(map[string][]arr.ContentFile)
	files := media.Files
	for _, file := range files {
		target := getSymlinkTarget(file.Path)
		if target != "" {
			file.IsSymlink = true
			dir, _ := filepath.Split(target)
			parent := filepath.Base(filepath.Clean(dir))
			uniqueParents[parent] = append(uniqueParents[parent], file)
		}
	}

	for parent, f := range uniqueParents {
		// Check stat
		// Check file stat first
		firstFile := f[0]
		// Read a tiny bit of the file
		if err := fileIsReadable(firstFile.Path); err != nil {
			r.logger.Debug().Msgf("Broken file found at: %s", parent)
			brokenFiles = append(brokenFiles, f...)
			continue
		}
	}
	if len(brokenFiles) == 0 {
		r.logger.Debug().Msgf("No broken files found for %s", media.Title)
		return nil
	}
	r.logger.Debug().Msgf("%d broken files found for %s", len(brokenFiles), media.Title)
	return brokenFiles
}

func (r *Repair) getZurgBrokenFiles(media arr.Content) []arr.ContentFile {
	// Use zurg setup to check file availability with zurg
	// This reduces bandwidth usage significantly

	brokenFiles := make([]arr.ContentFile, 0)
	uniqueParents := make(map[string][]arr.ContentFile)
	files := media.Files
	for _, file := range files {
		target := getSymlinkTarget(file.Path)
		if target != "" {
			file.IsSymlink = true
			dir, f := filepath.Split(target)
			parent := filepath.Base(filepath.Clean(dir))
			// Set target path folder/file.mkv
			file.TargetPath = f
			uniqueParents[parent] = append(uniqueParents[parent], file)
		}
	}
	// Access zurg url + symlink folder + first file(encoded)
	for parent, f := range uniqueParents {
		r.logger.Debug().Msgf("Checking %s", parent)
		encodedParent := url.PathEscape(parent)
		encodedFile := url.PathEscape(f[0].TargetPath)
		fullURL := fmt.Sprintf("%s/http/__all__/%s/%s", r.ZurgURL, encodedParent, encodedFile)
		// Check file stat first
		if _, err := os.Stat(f[0].Path); os.IsNotExist(err) {
			r.logger.Debug().Msgf("Broken symlink found: %s", fullURL)
			brokenFiles = append(brokenFiles, f...)
			continue
		}

		resp, err := http.Get(fullURL)
		if err != nil {
			r.logger.Debug().Err(err).Msgf("Failed to reach %s", fullURL)
			brokenFiles = append(brokenFiles, f...)
			continue
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			r.logger.Debug().Msgf("Failed to get download url for %s", fullURL)
			brokenFiles = append(brokenFiles, f...)
			continue
		}
		downloadUrl := resp.Request.URL.String()
		if downloadUrl != "" {
			r.logger.Debug().Msgf("Found download url: %s", downloadUrl)
		} else {
			r.logger.Debug().Msgf("Failed to get download url for %s", fullURL)
			brokenFiles = append(brokenFiles, f...)
			continue
		}
	}
	if len(brokenFiles) == 0 {
		r.logger.Debug().Msgf("No broken files found for %s", media.Title)
		return nil
	}
	r.logger.Debug().Msgf("%d broken files found for %s", len(brokenFiles), media.Title)
	return brokenFiles
}
