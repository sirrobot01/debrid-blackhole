package version

type Info struct {
	Version string `json:"version"`
	Channel string `json:"channel"`
}

var (
	Version = ""
	Channel = ""
)

func GetInfo() Info {
	return Info{
		Version: Version,
		Channel: Channel,
	}
}
