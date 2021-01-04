package main

import (
	"fmt"
	"os"

	"github.com/b582q9/go-textile-sapien/cmd"
)

func main() {
	err := cmd.Run()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
