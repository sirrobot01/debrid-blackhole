package cli

import (
	"fmt"
	"os"
)

var commands = map[string]Command{}

func Register(cmd Command) {
    commands[cmd.Name] = cmd
}

func showUsage() {
    fmt.Println("Usage: blackhole <command> [options]")
    fmt.Println("\nAvailable Commands:")
    for _, cmd := range commands {
        fmt.Printf("  %-12s %s\n", cmd.Name, cmd.Description)
    }
    fmt.Println("\nUse 'blackhole <command> --help' for more information about a command")
}

func Execute() error {
    if len(os.Args) < 2 {
        showUsage()
        return nil
    }

    commandName := os.Args[1]
    command, exists := commands[commandName]
    if !exists {
        if commandName == "--help" || commandName == "-h" {
            showUsage()
            return nil
        }
        return fmt.Errorf("unknown command %q", commandName)
    }

    return command.Execute(os.Args[2:])
}
