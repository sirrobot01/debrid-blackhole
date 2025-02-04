package arr

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
)

func (a *Arr) GetMedia(tvId string) ([]Content, error) {
	// Get series
	resp, err := a.Request(http.MethodGet, fmt.Sprintf("api/v3/series?tvdbId=%s", tvId), nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		// This is Radarr
		log.Println("Radarr detected")
		a.Type = Radarr
		return GetMovies(a, tvId)
	}
	a.Type = Sonarr
	defer resp.Body.Close()
	type series struct {
		Title string `json:"title"`
		Id    int    `json:"id"`
	}
	var data []series
	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
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

		type episode struct {
			Id            int `json:"id"`
			EpisodeFileID int `json:"episodeFileId"`
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
			files = append(files, ContentFile{
				FileId: file.Id,
				Path:   file.Path,
				Id:     eId,
			})
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
	defer resp.Body.Close()
	var movies []Movie
	if err = json.NewDecoder(resp.Body).Decode(&movies); err != nil {
		return nil, err
	}
	contents := make([]Content, 0)
	for _, movie := range movies {
		ct := Content{
			Title: movie.Title,
			Id:    movie.Id,
		}
		files := make([]ContentFile, 0)
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

func (a *Arr) SearchMissing(files []ContentFile) error {
	var payload interface{}

	ids := make([]int, 0)
	for _, f := range files {
		ids = append(ids, f.Id)
	}

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
		return fmt.Errorf("failed to search missing: %v", err)
	}
	if statusOk := strconv.Itoa(resp.StatusCode)[0] == '2'; !statusOk {
		return fmt.Errorf("failed to search missing. Status Code: %s", resp.Status)
	}
	return nil
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
