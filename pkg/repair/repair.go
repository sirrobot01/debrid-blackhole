package repair

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
	"github.com/sirrobot01/debrid-blackhole/internal/config"
	"github.com/sirrobot01/debrid-blackhole/internal/logger"
	"github.com/sirrobot01/debrid-blackhole/internal/request"
	"github.com/sirrobot01/debrid-blackhole/pkg/arr"
	"github.com/sirrobot01/debrid-blackhole/pkg/debrid/engine"
	"golang.org/x/sync/errgroup"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Repair struct {
	Jobs        map[string]*Job
	arrs        *arr.Storage
	deb         engine.Service
	duration    time.Duration
	runOnStart  bool
	ZurgURL     string
	IsZurg      bool
	autoProcess bool
	logger      zerolog.Logger
	filename    string
}

func New(arrs *arr.Storage) *Repair {
	cfg := config.GetConfig()
	duration, err := parseSchedule(cfg.Repair.Interval)
	if err != nil {
		duration = time.Hour * 24
	}
	r := &Repair{
		arrs:        arrs,
		logger:      logger.NewLogger("repair", cfg.LogLevel, os.Stdout),
		duration:    duration,
		runOnStart:  cfg.Repair.RunOnStart,
		ZurgURL:     cfg.Repair.ZurgURL,
		autoProcess: cfg.Repair.AutoProcess,
		filename:    filepath.Join(cfg.Path, "repair.json"),
	}
	if r.ZurgURL != "" {
		r.IsZurg = true
	}
	// Load jobs from file
	r.loadFromFile()

	return r
}

type JobStatus string

const (
	JobStarted   JobStatus = "started"
	JobPending   JobStatus = "pending"
	JobFailed    JobStatus = "failed"
	JobCompleted JobStatus = "completed"
)

type Job struct {
	ID          string                       `json:"id"`
	Arrs        []string                     `json:"arrs"`
	MediaIDs    []string                     `json:"media_ids"`
	StartedAt   time.Time                    `json:"created_at"`
	BrokenItems map[string][]arr.ContentFile `json:"broken_items"`
	Status      JobStatus                    `json:"status"`
	CompletedAt time.Time                    `json:"finished_at"`
	FailedAt    time.Time                    `json:"failed_at"`
	AutoProcess bool                         `json:"auto_process"`
	Recurrent   bool                         `json:"recurrent"`

	Error string `json:"error"`
}

func (j *Job) discordContext() string {
	format := `
		**ID**: %s
		**Arrs**: %s
		**Media IDs**: %s
		**Status**: %s
		**Started At**: %s
		**Completed At**: %s 
`

	dateFmt := "2006-01-02 15:04:05"

	return fmt.Sprintf(format, j.ID, strings.Join(j.Arrs, ","), strings.Join(j.MediaIDs, ", "), j.Status, j.StartedAt.Format(dateFmt), j.CompletedAt.Format(dateFmt))
}

func (r *Repair) getArrs(arrNames []string) []string {
	arrs := make([]string, 0)
	if len(arrNames) == 0 {
		// No specific arrs, get all
		// Also check if any arrs are set to skip repair
		_arrs := r.arrs.GetAll()
		for _, a := range _arrs {
			if a.SkipRepair {
				continue
			}
			arrs = append(arrs, a.Name)
		}
	} else {
		for _, name := range arrNames {
			a := r.arrs.Get(name)
			if a == nil || a.Host == "" || a.Token == "" {
				continue
			}
			arrs = append(arrs, a.Name)
		}
	}
	return arrs
}

func jobKey(arrNames []string, mediaIDs []string) string {
	return fmt.Sprintf("%s-%s", strings.Join(arrNames, ","), strings.Join(mediaIDs, ","))
}

func (r *Repair) reset(j *Job) {
	// Update job for rerun
	j.Status = JobStarted
	j.StartedAt = time.Now()
	j.CompletedAt = time.Time{}
	j.FailedAt = time.Time{}
	j.BrokenItems = nil
	j.Error = ""
	if j.Recurrent || j.Arrs == nil {
		j.Arrs = r.getArrs([]string{}) // Get new arrs
	}
}

func (r *Repair) newJob(arrsNames []string, mediaIDs []string) *Job {
	arrs := r.getArrs(arrsNames)
	return &Job{
		ID:        uuid.New().String(),
		Arrs:      arrs,
		MediaIDs:  mediaIDs,
		StartedAt: time.Now(),
		Status:    JobStarted,
	}
}

func (r *Repair) preRunChecks() error {
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

func (r *Repair) AddJob(arrsNames []string, mediaIDs []string, autoProcess, recurrent bool) error {
	key := jobKey(arrsNames, mediaIDs)
	job, ok := r.Jobs[key]
	if job != nil && job.Status == JobStarted {
		return fmt.Errorf("job already running")
	}
	if !ok {
		job = r.newJob(arrsNames, mediaIDs)
	}
	job.AutoProcess = autoProcess
	job.Recurrent = recurrent
	r.reset(job)
	r.Jobs[key] = job
	go r.saveToFile()
	err := r.repair(job)
	go r.saveToFile()
	return err
}

func (r *Repair) repair(job *Job) error {
	if err := r.preRunChecks(); err != nil {
		return err
	}

	// Create a new error group with context
	g, ctx := errgroup.WithContext(context.Background())

	// Use a mutex to protect concurrent access to brokenItems
	var mu sync.Mutex
	brokenItems := map[string][]arr.ContentFile{}

	for _, a := range job.Arrs {
		a := a // Capture range variable
		g.Go(func() error {
			var items []arr.ContentFile
			var err error

			if len(job.MediaIDs) == 0 {
				items, err = r.repairArr(job, a, "")
				if err != nil {
					r.logger.Error().Err(err).Msgf("Error repairing %s", a)
					return err
				}
			} else {
				for _, id := range job.MediaIDs {
					// Check if any other goroutine has failed
					select {
					case <-ctx.Done():
						return ctx.Err()
					default:
					}

					someItems, err := r.repairArr(job, a, id)
					if err != nil {
						r.logger.Error().Err(err).Msgf("Error repairing %s with ID %s", a, id)
						return err
					}
					items = append(items, someItems...)
				}
			}

			// Safely append the found items to the shared slice
			if len(items) > 0 {
				mu.Lock()
				brokenItems[a] = items
				mu.Unlock()
			}

			return nil
		})
	}

	// Wait for all goroutines to complete and check for errors
	if err := g.Wait(); err != nil {
		job.FailedAt = time.Now()
		job.Error = err.Error()
		job.Status = JobFailed
		job.CompletedAt = time.Now()
		go func() {
			if err := request.SendDiscordMessage("repair_failed", "error", job.discordContext()); err != nil {
				r.logger.Error().Msgf("Error sending discord message: %v", err)
			}
		}()
		return err
	}

	if len(brokenItems) == 0 {
		job.CompletedAt = time.Now()
		job.Status = JobCompleted

		go func() {
			if err := request.SendDiscordMessage("repair_complete", "success", job.discordContext()); err != nil {
				r.logger.Error().Msgf("Error sending discord message: %v", err)
			}
		}()

		return nil
	}

	job.BrokenItems = brokenItems
	if job.AutoProcess {
		// Job is already processed
		job.CompletedAt = time.Now() // Mark as completed
		job.Status = JobCompleted
		go func() {
			if err := request.SendDiscordMessage("repair_complete", "success", job.discordContext()); err != nil {
				r.logger.Error().Msgf("Error sending discord message: %v", err)
			}
		}()
	} else {
		job.Status = JobPending
		go func() {
			if err := request.SendDiscordMessage("repair_pending", "pending", job.discordContext()); err != nil {
				r.logger.Error().Msgf("Error sending discord message: %v", err)
			}
		}()
	}
	return nil
}

func (r *Repair) Start(ctx context.Context) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg := config.GetConfig()

	if r.runOnStart {
		r.logger.Info().Msgf("Running initial repair")
		go func() {
			if err := r.AddJob([]string{}, []string{}, r.autoProcess, true); err != nil {
				r.logger.Error().Err(err).Msg("Error running initial repair")
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
			if err := r.AddJob([]string{}, []string{}, r.autoProcess, true); err != nil {
				r.logger.Error().Err(err).Msg("Error running repair")
			}

			// If using time-of-day schedule, reset the ticker for next day
			if strings.Contains(cfg.Repair.Interval, ":") {
				ticker.Reset(r.duration)
			}

			r.logger.Info().Msgf("Next scheduled repair at %v", t.Add(r.duration).Format("15:04:05"))
		}
	}
}

func (r *Repair) repairArr(j *Job, _arr string, tmdbId string) ([]arr.ContentFile, error) {
	brokenItems := make([]arr.ContentFile, 0)
	a := r.arrs.Get(_arr)

	r.logger.Info().Msgf("Starting repair for %s", a.Name)
	media, err := a.GetMedia(tmdbId)
	if err != nil {
		r.logger.Info().Msgf("Failed to get %s media: %v", a.Name, err)
		return brokenItems, err
	}
	r.logger.Info().Msgf("Found %d %s media", len(media), a.Name)

	if len(media) == 0 {
		r.logger.Info().Msgf("No %s media found", a.Name)
		return brokenItems, nil
	}
	// Check first media to confirm mounts are accessible
	if !r.isMediaAccessible(media[0]) {
		r.logger.Info().Msgf("Skipping repair. Parent directory not accessible for. Check your mounts")
		return brokenItems, nil
	}

	// Create a new error group
	g, ctx := errgroup.WithContext(context.Background())

	// Limit concurrent goroutines
	g.SetLimit(runtime.NumCPU() * 4)

	// Mutex for brokenItems
	var mu sync.Mutex

	for _, m := range media {
		m := m // Create a new variable scoped to the loop iteration
		g.Go(func() error {
			// Check if context was canceled
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			items := r.getBrokenFiles(m)
			if items != nil {
				r.logger.Debug().Msgf("Found %d broken files for %s", len(items), m.Title)
				if j.AutoProcess {
					r.logger.Info().Msgf("Auto processing %d broken items for %s", len(items), m.Title)

					// Delete broken items

					if err := a.DeleteFiles(items); err != nil {
						r.logger.Debug().Msgf("Failed to delete broken items for %s: %v", m.Title, err)
					}

					// Search for missing items
					if err := a.SearchMissing(items); err != nil {
						r.logger.Debug().Msgf("Failed to search missing items for %s: %v", m.Title, err)
					}
				}

				mu.Lock()
				brokenItems = append(brokenItems, items...)
				mu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return brokenItems, err
	}

	r.logger.Info().Msgf("Repair completed for %s. %d broken items found", a.Name, len(brokenItems))
	return brokenItems, nil
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
	client := &http.Client{
		Timeout: 0,
		Transport: &http.Transport{
			TLSHandshakeTimeout: 60 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   20 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
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

		resp, err := client.Get(fullURL)
		if err != nil {
			r.logger.Debug().Err(err).Msgf("Failed to reach %s", fullURL)
			brokenFiles = append(brokenFiles, f...)
			continue
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			r.logger.Debug().Msgf("Failed to get download url for %s", fullURL)
			resp.Body.Close()
			brokenFiles = append(brokenFiles, f...)
			continue
		}

		downloadUrl := resp.Request.URL.String()
		resp.Body.Close()

		if downloadUrl != "" {
			r.logger.Trace().Msgf("Found download url: %s", downloadUrl)
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

func (r *Repair) GetJob(id string) *Job {
	for _, job := range r.Jobs {
		if job.ID == id {
			return job
		}
	}
	return nil
}

func (r *Repair) GetJobs() []*Job {
	jobs := make([]*Job, 0)
	for _, job := range r.Jobs {
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].StartedAt.After(jobs[j].StartedAt)
	})

	return jobs
}

func (r *Repair) ProcessJob(id string) error {
	job := r.GetJob(id)
	if job == nil {
		return fmt.Errorf("job %s not found", id)
	}
	if job.Status != JobPending {
		return fmt.Errorf("job %s not pending", id)
	}
	if job.StartedAt.IsZero() {
		return fmt.Errorf("job %s not started", id)
	}
	if !job.CompletedAt.IsZero() {
		return fmt.Errorf("job %s already completed", id)
	}
	if !job.FailedAt.IsZero() {
		return fmt.Errorf("job %s already failed", id)
	}

	brokenItems := job.BrokenItems
	if len(brokenItems) == 0 {
		r.logger.Info().Msgf("No broken items found for job %s", id)
		job.CompletedAt = time.Now()
		job.Status = JobCompleted
		return nil
	}

	// Create a new error group
	g := new(errgroup.Group)

	for arrName, items := range brokenItems {
		items := items
		arrName := arrName
		g.Go(func() error {
			a := r.arrs.Get(arrName)
			if a == nil {
				r.logger.Error().Msgf("Arr %s not found", arrName)
				return nil
			}

			if err := a.DeleteFiles(items); err != nil {
				r.logger.Error().Err(err).Msgf("Failed to delete broken items for %s", arrName)
				return nil

			}
			// Search for missing items
			if err := a.SearchMissing(items); err != nil {
				r.logger.Error().Err(err).Msgf("Failed to search missing items for %s", arrName)
				return nil
			}
			return nil

		})
	}

	if err := g.Wait(); err != nil {
		job.FailedAt = time.Now()
		job.Error = err.Error()
		job.CompletedAt = time.Now()
		job.Status = JobFailed
		return err
	}

	job.CompletedAt = time.Now()
	job.Status = JobCompleted

	return nil
}

func (r *Repair) saveToFile() {
	// Save jobs to file
	data, err := json.Marshal(r.Jobs)
	if err != nil {
		r.logger.Debug().Err(err).Msg("Failed to marshal jobs")
	}
	err = os.WriteFile(r.filename, data, 0644)
}

func (r *Repair) loadFromFile() {
	data, err := os.ReadFile(r.filename)
	if err != nil && os.IsNotExist(err) {
		r.Jobs = make(map[string]*Job)
		return
	}
	jobs := make(map[string]*Job)
	err = json.Unmarshal(data, &jobs)
	if err != nil {
		r.logger.Trace().Err(err).Msg("Failed to unmarshal jobs; resetting")
		r.Jobs = make(map[string]*Job)
		return
	}
	r.Jobs = jobs
}

func (r *Repair) DeleteJobs(ids []string) {
	for _, id := range ids {
		if id == "" {
			continue
		}
		for k, job := range r.Jobs {
			if job.ID == id {
				delete(r.Jobs, k)
			}
		}
	}
	go r.saveToFile()
}
