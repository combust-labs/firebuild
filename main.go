package main

import (
	"fmt"
	"os"

	"github.com/combust-labs/firebuild/cmd/baseos"
	"github.com/combust-labs/firebuild/cmd/rootfs"
	"github.com/combust-labs/firebuild/cmd/run"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "firebuild",
	Short: "firebuild",
	Long:  ``,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
		os.Exit(1)
	},
}

func init() {
	rootCmd.AddCommand(baseos.Command)
	rootCmd.AddCommand(rootfs.Command)
	rootCmd.AddCommand(run.Command)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
