package arr

import (
	"encoding/json"
	"fmt"
	"net/http"
)

func (a *Arr) GetMedia(tvId string) ([]Content, error) {
	// Get series
	resp, err := a.Request(http.MethodGet, fmt.Sprintf("api/v3/series?tvdbId=%s", tvId), nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		// This is Radarr
		repairLogger.Info().Msg("Radarr detected")
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
		files := make([]contentFile, 0)
		for _, file := range seriesFiles {
			files = append(files, contentFile{
				Id:   file.Id,
				Path: file.Path,
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
		files := make([]contentFile, 0)
		files = append(files, contentFile{
			Id:   movie.MovieFile.Id,
			Path: movie.MovieFile.Path,
		})
		ct.Files = files
		contents = append(contents, ct)
	}
	return contents, nil
}

func (a *Arr) DeleteFile(id int) error {
	switch a.Type {
	case Sonarr:
		_, err := a.Request(http.MethodDelete, fmt.Sprintf("api/v3/episodefile/%d", id), nil)
		if err != nil {
			return err
		}
	case Radarr:
		_, err := a.Request(http.MethodDelete, fmt.Sprintf("api/v3/moviefile/%d", id), nil)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown arr type: %s", a.Type)
	}
	return nil
}
