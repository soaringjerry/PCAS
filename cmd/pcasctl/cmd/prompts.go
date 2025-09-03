package cmd

import (
    "bufio"
    "fmt"
    "os"
    "strings"
)

// askInput prompts the user for input with a default value
func askInput(prompt, def string) string {
    reader := bufio.NewReader(os.Stdin)
    if def != "" {
        fmt.Printf("%s [%s]: ", prompt, def)
    } else {
        fmt.Printf("%s: ", prompt)
    }
    text, _ := reader.ReadString('\n')
    text = strings.TrimSpace(text)
    if text == "" {
        return def
    }
    return text
}

// askYesNo prompts the user with a yes/no question with default
func askYesNo(prompt string, defYes bool) bool {
    def := "y"
    if !defYes {
        def = "n"
    }
    reader := bufio.NewReader(os.Stdin)
    fmt.Printf("%s [y/n, default=%s]: ", prompt, def)
    text, _ := reader.ReadString('\n')
    text = strings.TrimSpace(strings.ToLower(text))
    if text == "" {
        return defYes
    }
    return text == "y" || text == "yes"
}

