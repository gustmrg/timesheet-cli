package cli

import (
	"fmt"
	"os"

	"github.com/gustmrg/timesheet-cli/internal/updater"
	"github.com/spf13/cobra"
)

const defaultUpgradeAPIURL = "https://api.github.com/repos/gustmrg/timesheet-cli/releases/latest"

func (a *app) upgradeCommand() *cobra.Command {
	return &cobra.Command{Use: "upgrade", Short: "Upgrade the CLI to the latest release", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		executable, err := os.Executable()
		if err != nil {
			return fail("upgrade_failed", "determine current executable", 5, err)
		}
		client, err := updater.New(updater.Config{
			APIURL:         upgradeAPIURL(),
			CurrentVersion: a.version,
			Executable:     executable,
			Token:          githubToken(),
		})
		if err != nil {
			return fail("upgrade_failed", err.Error(), 5, err)
		}
		result, err := client.Update(cmd.Context())
		if err != nil {
			return fail("upgrade_failed", err.Error(), 5, err)
		}
		return a.success(result, func() {
			if result.Updated {
				fmt.Fprintf(a.out, "upgraded timesheet from v%s to v%s\n", result.CurrentVersion, result.LatestVersion)
				fmt.Fprintf(a.out, "executable: %s\n", result.Executable)
				return
			}
			fmt.Fprintf(a.out, "timesheet v%s is already up to date\n", result.CurrentVersion)
		})
	}}
}

func upgradeAPIURL() string {
	if value := os.Getenv("TIMESHEET_UPGRADE_API_URL"); value != "" {
		return value
	}
	return defaultUpgradeAPIURL
}

func githubToken() string {
	if value := os.Getenv("GH_TOKEN"); value != "" {
		return value
	}
	return os.Getenv("GITHUB_TOKEN")
}
