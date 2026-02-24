package arr

// Webhook event type constants as sent by Sonarr/Radarr v4.
const (
	EventTypeTest              = "Test"
	EventTypeDownload          = "Download"
	EventTypeEpisodeFileDelete = "EpisodeFileDelete"
	EventTypeMovieFileDelete   = "MovieFileDelete"
	EventTypeRename            = "Rename"
	EventTypeSeriesDelete      = "SeriesDelete"
	EventTypeMovieDelete       = "MovieDelete"
)

// WebhookPayload represents the common payload sent by Sonarr/Radarr v4 webhooks.
type WebhookPayload struct {
	EventType    string `json:"eventType"`
	InstanceName string `json:"instanceName"`
	DownloadId   string `json:"downloadId,omitempty"` // = infohash for torrent grabs
	IsUpgrade    bool   `json:"isUpgrade,omitempty"`
	SourceFolder string `json:"sourceFolder,omitempty"`
	DeleteReason string `json:"deleteReason,omitempty"`
	DeletedFiles bool   `json:"deletedFiles,omitempty"`

	// Sonarr-specific fields
	Series              *WebhookSeries  `json:"series,omitempty"`
	EpisodeFile         *WebhookFile    `json:"episodeFile,omitempty"`
	EpisodeFiles        []WebhookFile   `json:"episodeFiles,omitempty"`
	RenamedEpisodeFiles []WebhookRename `json:"renamedEpisodeFiles,omitempty"`

	// Radarr-specific fields
	Movie             *WebhookMovie   `json:"movie,omitempty"`
	MovieFile         *WebhookFile    `json:"movieFile,omitempty"`
	RenamedMovieFiles []WebhookRename `json:"renamedMovieFiles,omitempty"`
}

// WebhookSeries carries series-level information (used in SeriesDelete events).
type WebhookSeries struct {
	Id   int    `json:"id"`
	Path string `json:"path"`
}

// WebhookMovie carries movie-level information (used in MovieDelete events).
type WebhookMovie struct {
	Id         int    `json:"id"`
	FolderPath string `json:"folderPath"`
}

// WebhookFile carries file information within a webhook payload.
type WebhookFile struct {
	Id           int    `json:"id"`
	Path         string `json:"path"`
	RelativePath string `json:"relativePath"`
}

// WebhookRename pairs a previous path with its new path for rename events.
type WebhookRename struct {
	PreviousPath string `json:"previousPath"`
	Path         string `json:"path"`
}

// ManagedPaths returns the current managed file paths described by the payload:
// imported paths for Download, the deleted file for EpisodeFileDelete/MovieFileDelete,
// and new paths for Rename events.
func (p *WebhookPayload) ManagedPaths() []string {
	var paths []string
	switch p.EventType {
	case EventTypeDownload:
		if p.EpisodeFile != nil && p.EpisodeFile.Path != "" {
			paths = append(paths, p.EpisodeFile.Path)
		}
		for _, f := range p.EpisodeFiles {
			if f.Path != "" {
				paths = append(paths, f.Path)
			}
		}
		if p.MovieFile != nil && p.MovieFile.Path != "" {
			paths = append(paths, p.MovieFile.Path)
		}
	case EventTypeEpisodeFileDelete:
		if p.EpisodeFile != nil && p.EpisodeFile.Path != "" {
			paths = append(paths, p.EpisodeFile.Path)
		}
	case EventTypeMovieFileDelete:
		if p.MovieFile != nil && p.MovieFile.Path != "" {
			paths = append(paths, p.MovieFile.Path)
		}
	case EventTypeRename:
		for _, r := range p.RenamedEpisodeFiles {
			if r.Path != "" {
				paths = append(paths, r.Path)
			}
		}
		for _, r := range p.RenamedMovieFiles {
			if r.Path != "" {
				paths = append(paths, r.Path)
			}
		}
	}
	return paths
}

// PreviousPaths returns the old file paths for Rename events.
func (p *WebhookPayload) PreviousPaths() []string {
	var paths []string
	for _, r := range p.RenamedEpisodeFiles {
		if r.PreviousPath != "" {
			paths = append(paths, r.PreviousPath)
		}
	}
	for _, r := range p.RenamedMovieFiles {
		if r.PreviousPath != "" {
			paths = append(paths, r.PreviousPath)
		}
	}
	return paths
}
