package arr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

type episode struct {
	Id            int `json:"id"`
	EpisodeFileID int `json:"episodeFileId"`
}

func (a *Arr) GetMedia(mediaId string) ([]Content, error) {
	// Get series
	if a.Type == Radarr {
		return GetMovies(a, mediaId)
	}
	// This is likely Sonarr
	resp, err := a.Request(http.MethodGet, fmt.Sprintf("api/v3/series?tvdbId=%s", mediaId), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		// This is likely Radarr
		return GetMovies(a, mediaId)
	}
	a.Type = Sonarr

	type series struct {
		Title string `json:"title"`
		Id    int    `json:"id"`
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get series: %s", resp.Status)
	}
	var data []series
	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("failed to decode series: %v", err)
	}
	// Get series files
	contents := make([]Content, 0)
	for _, d := range data {
		resp, err = a.Request(http.MethodGet, fmt.Sprintf("api/v3/episodefile?seriesId=%d", d.Id), nil)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		var seriesFiles []seriesFile
		if err = json.NewDecoder(resp.Body).Decode(&seriesFiles); err != nil {
			continue
		}
		ct := Content{
			Title: d.Title,
			Id:    d.Id,
		}
		resp, err = a.Request(http.MethodGet, fmt.Sprintf("api/v3/episode?seriesId=%d", d.Id), nil)
		if err != nil {
			continue
		}
		defer resp.Body.Close()
		var episodes []episode
		if err = json.NewDecoder(resp.Body).Decode(&episodes); err != nil {
			continue
		}
		episodeFileIDMap := make(map[int]int)
		for _, e := range episodes {
			episodeFileIDMap[e.EpisodeFileID] = e.Id
		}
		files := make([]ContentFile, 0)
		for _, file := range seriesFiles {
			eId, ok := episodeFileIDMap[file.Id]
			if !ok {
				eId = 0
			}
			if file.Id == 0 || file.Path == "" {
				// Skip files without path
				continue
			}
			files = append(files, ContentFile{
				FileId: file.Id,
				Path:   file.Path,
				Id:     eId,
			})
		}
		if len(files) == 0 {
			// Skip series without files
			continue
		}
		ct.Files = files
		contents = append(contents, ct)
	}
	return contents, nil
}

func GetMovies(a *Arr, tvId string) ([]Content, error) {
	resp, err := a.Request(http.MethodGet, fmt.Sprintf("api/v3/movie?tmdbId=%s", tvId), nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		// This is likely Lidarr or Readarr
		return nil, fmt.Errorf("failed to get movies: %s", resp.Status)
	}
	a.Type = Radarr
	defer resp.Body.Close()
	var movies []Movie
	if err = json.NewDecoder(resp.Body).Decode(&movies); err != nil {
		return nil, fmt.Errorf("failed to decode movies: %v", err)
	}
	contents := make([]Content, 0)
	for _, movie := range movies {
		ct := Content{
			Title: movie.Title,
			Id:    movie.Id,
		}
		files := make([]ContentFile, 0)
		if movie.MovieFile.Id == 0 || movie.MovieFile.Path == "" {
			// Skip movies without files
			continue
		}
		files = append(files, ContentFile{
			FileId: movie.MovieFile.Id,
			Id:     movie.Id,
			Path:   movie.MovieFile.Path,
		})
		ct.Files = files
		contents = append(contents, ct)
	}
	return contents, nil
}

func (a *Arr) search(ids []int) error {
	var payload interface{}
	switch a.Type {
	case Sonarr:
		payload = struct {
			Name       string `json:"name"`
			EpisodeIds []int  `json:"episodeIds"`
		}{
			Name:       "EpisodeSearch",
			EpisodeIds: ids,
		}
	case Radarr:
		payload = struct {
			Name     string `json:"name"`
			MovieIds []int  `json:"movieIds"`
		}{
			Name:     "MoviesSearch",
			MovieIds: ids,
		}
	default:
		return fmt.Errorf("unknown arr type: %s", a.Type)
	}

	resp, err := a.Request(http.MethodPost, "api/v3/command", payload)
	if err != nil {
		return fmt.Errorf("failed to automatic search: %v", err)
	}
	if statusOk := strconv.Itoa(resp.StatusCode)[0] == '2'; !statusOk {
		return fmt.Errorf("failed to automatic search. Status Code: %s", resp.Status)
	}
	return nil
}

func (a *Arr) SearchMissing(files []ContentFile) error {

	ids := make([]int, 0)
	for _, f := range files {
		ids = append(ids, f.Id)
	}

	if len(ids) == 0 {
		return nil
	}
	return a.search(ids)
}

func (a *Arr) DeleteFiles(files []ContentFile) error {
	ids := make([]int, 0)
	for _, f := range files {
		ids = append(ids, f.FileId)
	}
	var payload interface{}
	switch a.Type {
	case Sonarr:
		payload = struct {
			EpisodeFileIds []int `json:"episodeFileIds"`
		}{
			EpisodeFileIds: ids,
		}
		_, err := a.Request(http.MethodDelete, "api/v3/episodefile/bulk", payload)
		if err != nil {
			return err
		}
	case Radarr:
		payload = struct {
			MovieFileIds []int `json:"movieFileIds"`
		}{
			MovieFileIds: ids,
		}
		_, err := a.Request(http.MethodDelete, "api/v3/moviefile/bulk", payload)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown arr type: %s", a.Type)
	}
	return nil
}
