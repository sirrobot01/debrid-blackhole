package manager

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/sirrobot01/decypharr/internal/config"
	"github.com/sirrobot01/decypharr/pkg/arr"
	"github.com/sirrobot01/decypharr/pkg/storage"
)

// GetArrStorage returns the arr storage used to look up configured ARR instances.
func (m *Manager) GetArrStorage() *arr.Storage {
	return m.arr
}

// RegisterArrWebhooks registers the Decypharr webhook notification in every configured ARR
// instance that has a host and token. Called once at startup; each registration runs in its
// own goroutine so it never blocks the manager startup sequence.
func (m *Manager) RegisterArrWebhooks() {
	cfg := config.Get()

	baseURL := cfg.AppURL
	if baseURL == "" {
		bindAddress := cfg.BindAddress
		if bindAddress == "" || bindAddress == "0.0.0.0" {
			bindAddress = "localhost"
		}
		baseURL = fmt.Sprintf("http://%s:%s", bindAddress, cfg.Port)
	}
	urlBase := strings.TrimRight(cfg.URLBase, "/")

	for _, a := range m.arr.GetAll() {
		if a.Host == "" || a.Token == "" {
			continue
		}
		webhookURL := fmt.Sprintf("%s%s/webhooks/arr?arr=%s", baseURL, urlBase, a.Name)
		go func(a *arr.Arr, webhookURL string) {
			if err := a.RegisterWebhook(webhookURL); err != nil {
				m.logger.Warn().Err(err).
					Str("arr", a.Name).
					Str("webhook_url", webhookURL).
					Msg("Failed to register arr webhook")
				return
			}
			m.logger.Info().
				Str("arr", a.Name).
				Str("webhook_url", webhookURL).
				Msg("Registered arr webhook")
		}(a, webhookURL)
	}
}

// syncArrFiles queries the ARR import history at startup and upserts an ArrFile for
// each entry, covering media imported before webhooks were active or during downtime.
// On first run, fetches from epoch. On subsequent runs, fetches only since the last
// processed event date, making the sync incremental and fast.
// Runs asynchronously; idempotent (UpsertArrFile overwrites existing records with the same path).
func (m *Manager) syncArrFiles() {
	for _, a := range m.arr.GetAll() {
		if a.Host == "" || a.Token == "" {
			continue
		}
		go func(a *arr.Arr) {
			downloadFolder := config.Get().DownloadFolder
			since, _ := m.storage.GetArrFilesLastEventDate(a.Name)
			records := a.GetImportHistorySince(since)
			var maxDate time.Time
			count := 0
			for _, r := range records {
				managedPath := r.Data["importedPath"]
				if managedPath == "" {
					continue
				}
				// Prefer sourceFolder when available; fall back to storage lookup
				// for older ARR versions that don't populate it in history data.
				if sourceFolder := r.Data["sourceFolder"]; sourceFolder != "" {
					if !isDecypharrSource(sourceFolder, downloadFolder) {
						continue
					}
				} else if _, err := m.storage.Get(strings.ToLower(r.DownloadID)); err != nil {
					continue
				}
				ref := &storage.ArrFile{
					ArrName:     a.Name,
					ManagedPath: managedPath,
					InfoHash:    strings.ToLower(r.DownloadID),
					FileName:    filepath.Base(managedPath),
				}
				if err := m.storage.UpsertArrFile(ref); err != nil {
					m.logger.Debug().Err(err).
						Str("arr", a.Name).
						Str("managed_path", managedPath).
						Msg("Failed to upsert arr file during sync")
					continue
				}
				if r.Date.After(maxDate) {
					maxDate = r.Date
				}
				count++
			}
			if !maxDate.IsZero() {
				if err := m.storage.SetArrFilesLastEventDate(a.Name, maxDate); err != nil {
					m.logger.Debug().Err(err).Str("arr", a.Name).Msg("Failed to update last event date")
				}
			}
			m.logger.Debug().
				Str("arr", a.Name).
				Int("files", count).
				Msg("Synced arr files from history")
		}(a)
	}
}

// isDecyphrrSource reports whether sourceFolder is nested under the configured download folder,
// confirming the import originated from Decypharr and not another download client.
func isDecypharrSource(sourceFolder, downloadFolder string) bool {
	if sourceFolder == "" || downloadFolder == "" {
		return false
	}
	dl := filepath.Clean(downloadFolder)
	return strings.HasPrefix(filepath.Clean(sourceFolder), dl+string(filepath.Separator))
}

// HandleArrImport is called on Sonarr/Radarr "Download" webhook events.
// It upserts an ArrFile for each managed path in the payload,
// but only when the import originated from Decypharr's download folder.
func (m *Manager) HandleArrImport(arrName string, payload *arr.WebhookPayload) {
	log := m.logger.With().
		Str("component", "arr_webhook").
		Str("arr", arrName).
		Str("event", payload.EventType).
		Logger()

	if !isDecypharrSource(payload.SourceFolder, config.Get().DownloadFolder) {
		log.Debug().Str("source_folder", payload.SourceFolder).Msg("Not a Decypharr import, skipping")
		return
	}

	for _, path := range payload.ManagedPaths() {
		ref := &storage.ArrFile{
			ArrName:     arrName,
			ManagedPath: path,
			InfoHash:    strings.ToLower(payload.DownloadId),
			FileName:    filepath.Base(path),
		}
		if err := m.storage.UpsertArrFile(ref); err != nil {
			log.Error().Err(err).Str("managed_path", path).Msg("Failed to upsert arr file")
			continue
		}
		log.Debug().Str("managed_path", path).Str("infohash", ref.InfoHash).Msg("Arr file upserted")
	}
}

// HandleArrDelete is called on "EpisodeFileDelete" / "MovieFileDelete" events.
// It removes the ArrFile, then cleans up the debrid entry if no files remain for its infohash.
func (m *Manager) HandleArrDelete(arrName string, payload *arr.WebhookPayload) {
	log := m.logger.With().
		Str("component", "arr_webhook").
		Str("arr", arrName).
		Str("event", payload.EventType).
		Logger()

	a := m.arr.Get(arrName)
	allowDelete := a != nil && a.AllowDelete

	for _, path := range payload.ManagedPaths() {
		ref, err := m.storage.DeleteArrFile(path)
		if err != nil {
			log.Error().Err(err).Str("managed_path", path).Msg("Failed to delete arr file")
			continue
		}
		if ref == nil {
			// No file stored – webhook may have been configured after the initial import.
			// Fall back to DownloadId when available.
			if payload.DownloadId != "" && allowDelete {
				log.Debug().Str("managed_path", path).Msg("No arr file found, falling back to download ID")
				m.deleteOrphanedEntry(strings.ToLower(payload.DownloadId))
			} else {
				log.Debug().Str("managed_path", path).Msg("No arr file found, skipping")
			}
			continue
		}
		if allowDelete {
			m.deleteOrphanedEntry(ref.InfoHash)
		}
	}
}

// HandleArrRename is called on "Rename" webhook events.
// It migrates ArrFiles from previous paths to their new paths.
func (m *Manager) HandleArrRename(arrName string, payload *arr.WebhookPayload) {
	log := m.logger.With().
		Str("component", "arr_webhook").
		Str("arr", arrName).
		Str("event", payload.EventType).
		Logger()

	prevPaths := payload.PreviousPaths()
	newPaths := payload.ManagedPaths()

	for i, prev := range prevPaths {
		ref, err := m.storage.DeleteArrFile(prev)
		if err != nil {
			log.Error().Err(err).Str("previous_path", prev).Msg("Failed to delete old arr file")
			continue
		}
		if ref == nil {
			log.Debug().Str("previous_path", prev).Msg("No previous arr file found")
			continue
		}
		if i >= len(newPaths) {
			log.Warn().Str("previous_path", prev).Msg("No corresponding new path for rename")
			continue
		}
		newRef := &storage.ArrFile{
			ArrName:     ref.ArrName,
			ManagedPath: newPaths[i],
			InfoHash:    ref.InfoHash,
			FileName:    ref.FileName,
		}
		if err := m.storage.UpsertArrFile(newRef); err != nil {
			log.Error().Err(err).Str("new_path", newPaths[i]).Msg("Failed to upsert new arr file")
			continue
		}
		log.Debug().Str("previous_path", prev).Str("new_path", newPaths[i]).Msg("Arr file renamed")
	}
}

// HandleArrSeriesDelete handles "SeriesDelete" events by cleaning up all files under the series folder.
// Skipped when deletedFiles is false (series removed from Sonarr but files kept on disk).
func (m *Manager) HandleArrSeriesDelete(arrName string, payload *arr.WebhookPayload) {
	if payload.Series == nil || payload.Series.Path == "" || !payload.DeletedFiles {
		return
	}
	log := m.logger.With().
		Str("component", "arr_webhook").
		Str("arr", arrName).
		Str("event", payload.EventType).
		Logger()
	a := m.arr.Get(arrName)
	m.handleArrFolderDelete(log, payload.Series.Path, a != nil && a.AllowDelete)
}

// HandleArrMovieDelete handles "MovieDelete" events by cleaning up all files under the movie folder.
// Skipped when deletedFiles is false (movie removed from Radarr but files kept on disk).
func (m *Manager) HandleArrMovieDelete(arrName string, payload *arr.WebhookPayload) {
	if payload.Movie == nil || payload.Movie.FolderPath == "" || !payload.DeletedFiles {
		return
	}
	log := m.logger.With().
		Str("component", "arr_webhook").
		Str("arr", arrName).
		Str("event", payload.EventType).
		Logger()
	a := m.arr.Get(arrName)
	m.handleArrFolderDelete(log, payload.Movie.FolderPath, a != nil && a.AllowDelete)
}

// handleArrFolderDelete finds all arr files under folderPath and cleans up each one.
// Entry deletion is gated on allowDelete.
func (m *Manager) handleArrFolderDelete(log zerolog.Logger, folderPath string, allowDelete bool) {
	log = log.With().Str("folder_path", folderPath).Logger()

	refs, err := m.storage.FindArrFilesByFolder(folderPath)
	if err != nil {
		log.Error().Err(err).Msg("Failed to find arr files by folder")
		return
	}
	if len(refs) == 0 {
		log.Debug().Msg("No arr files found for folder, nothing to clean up")
		return
	}

	infohashes := make(map[string]struct{})
	for _, ref := range refs {
		if _, err := m.storage.DeleteArrFile(ref.ManagedPath); err != nil {
			log.Error().Err(err).Str("managed_path", ref.ManagedPath).Msg("Failed to delete arr file")
			continue
		}
		infohashes[ref.InfoHash] = struct{}{}
	}
	if allowDelete {
		for infohash := range infohashes {
			m.deleteOrphanedEntry(infohash)
		}
	}
}

// deleteOrphanedEntry removes the Decypharr entry for infohash only when no ArrFile
// still references it, ensuring multi-file torrents are not deleted prematurely.
func (m *Manager) deleteOrphanedEntry(infohash string) {
	log := m.logger.With().
		Str("component", "arr_webhook").
		Str("infohash", infohash).
		Logger()

	remaining, err := m.storage.FindArrFilesByInfoHash(infohash)
	if err != nil {
		log.Error().Err(err).Msg("Failed to find remaining arr files")
		return
	}
	if len(remaining) > 0 {
		log.Debug().Int("remaining_files", len(remaining)).Msg("Entry still referenced, skipping cleanup")
		return
	}

	if _, err := m.GetEntry(infohash); err != nil {
		log.Debug().Msg("Entry not found in storage, nothing to clean up")
		return
	}

	log.Info().Msg("No ARR files remaining, deleting entry")
	if err := m.DeleteEntry(infohash, true); err != nil {
		log.Error().Err(err).Msg("Failed to delete entry")
	}
}
