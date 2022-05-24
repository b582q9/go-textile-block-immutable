package cmd

import (
	"fmt"

	"github.com/b582q9/go-textile-sapien/common"
)

func Version(git bool) error {
	if git {
		fmt.Println("go-textile-sapien version " + common.GitSummary)
	} else {
		fmt.Println("go-textile-sapien version v" + common.Version)
	}
	return nil
}
