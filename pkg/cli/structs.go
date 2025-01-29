package cli

type Command struct {
	Name        string
	Description string
	Execute     func([]string) error
}
