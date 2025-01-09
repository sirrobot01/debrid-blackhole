package arr

type Movie struct {
	Title         string `json:"title"`
	OriginalTitle string `json:"originalTitle"`
	Path          string `json:"path"`
	MovieFile     struct {
		MovieId      int    `json:"movieId"`
		RelativePath string `json:"relativePath"`
		Path         string `json:"path"`
		Size         int    `json:"size"`
		Id           int    `json:"id"`
	} `json:"movieFile"`
	Id int `json:"id"`
}

type contentFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Id   int    `json:"id"`
}

type Content struct {
	Title string        `json:"title"`
	Id    int           `json:"id"`
	Files []contentFile `json:"files"`
}

type seriesFile struct {
	SeriesId     int    `json:"seriesId"`
	SeasonNumber int    `json:"seasonNumber"`
	Path         string `json:"path"`
	Id           int    `json:"id"`
}
