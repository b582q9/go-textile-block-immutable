package cmd

import (
	"fmt"

	"github.com/b582q9/go-textile-block-immutable/core"
)

func InitCommand(config core.InitConfig) error {
	if err := core.InitRepo(config); err != nil {
		return fmt.Errorf("initialize failed: %s", err)
	}
	fmt.Printf("Initialized account with address %s\n", config.Account.Address())
	return nil
}
