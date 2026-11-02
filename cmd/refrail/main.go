package main

import (
	"os"
	"strconv"

	"refrail/internal/app"
)

func main() {
	code := app.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	if path := os.Getenv("REFRAIL_RESULT"); path != "" && (len(os.Args) > 1 && (os.Args[1] == "commit-msg" || os.Args[1] == "pre-push")) {
		if err := os.WriteFile(path, []byte(strconv.Itoa(code)), 0600); err != nil {
			os.Exit(2)
		}
	}
	os.Exit(code)
}
