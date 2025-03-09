package arr

type Movie struct {
	Title         string `json:"title"`
	OriginalTitle string `json:"originalTitle"`
	Path          string `json:"path"`
	MovieFile     struct {
		MovieId      int    `json:"movieId"`
		RelativePath string `json:"relativePath"`
		Path         string `json:"path"`
		Id           int    `json:"id"`
	} `json:"movieFile"`
	Id int `json:"id"`
}

type ContentFile struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	Id           int    `json:"id"`
	FileId       int    `json:"fileId"`
	TargetPath   string `json:"targetPath"`
	IsSymlink    bool   `json:"isSymlink"`
	IsBroken     bool   `json:"isBroken"`
	SeasonNumber int    `json:"seasonNumber"`
}

type Content struct {
	Title string        `json:"title"`
	Id    int           `json:"id"`
	Files []ContentFile `json:"files"`
}

type seriesFile struct {
	SeriesId     int    `json:"seriesId"`
	SeasonNumber int    `json:"seasonNumber"`
	Path         string `json:"path"`
	Id           int    `json:"id"`
}
