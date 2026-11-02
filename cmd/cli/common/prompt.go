package common

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// ConfirmationPrompt prompts the user for a yes/no answer and returns true for 'y', false for 'n'.
func ConfirmationPrompt(prompt string) bool {
	reader := bufio.NewReader(os.Stdin)

	for {
		fmt.Printf("%s [y/n] ", prompt)
		input, _ := reader.ReadString('\n')
		input = strings.ToLower(strings.TrimSpace(input))

		switch input {
		case "y", "yes":
			return true
		case "n", "no":
			return false
		default:
			fmt.Println(`Invalid input. Please enter "y" or "n".`)
		}
	}
}
