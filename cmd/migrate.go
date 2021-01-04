package cmd

import (
	"fmt"

	"github.com/b582q9/go-textile-sapien/core"
)

// Grab the repo path and migrate it to the latest version, passing the decryption pincode
func Migrate(repoPath string, pinCode string) error {
	if err := core.MigrateRepo(core.MigrateConfig{
		PinCode:  pinCode,
		RepoPath: repoPath,
	}); err != nil {
		return fmt.Errorf("migrate repo: %s", err)
	}
	fmt.Println("Repo was successfully migrated")
	return nil
}
