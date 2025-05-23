package common

import (
	"fmt"

	"github.com/skupperproject/skupper/internal/cmd/skupper/common/utils"
	"github.com/spf13/cobra"
)

type SkupperCommand interface {
	NewClient(cobraCommand *cobra.Command, args []string)
	ValidateInput(args []string) error
	InputToOptions()
	Run() error
	WaitUntil() error
}

type SkupperCmdDescription struct {
	Use     string
	Short   string
	Long    string
	Example string
}

func ConfigureCobraCommand(configuredPlatform Platform, description SkupperCmdDescription, kubeImpl SkupperCommand, nonKubeImpl SkupperCommand) *cobra.Command {
	var skupperCommand SkupperCommand
	var platform string

	cmd := cobra.Command{
		Use:     description.Use,
		Short:   description.Short,
		Long:    description.Long,
		Example: description.Example,
		PreRunE: func(cmd *cobra.Command, args []string) error {

			platform = string(configuredPlatform)
			if cmd.Flag("platform") != nil && cmd.Flag("platform").Value.String() != "" {
				platform = cmd.Flag("platform").Value.String()
			}

			switch platform {
			case "kubernetes":
				skupperCommand = kubeImpl
			case "podman", "docker", "linux":
				skupperCommand = nonKubeImpl
			default:
				return fmt.Errorf("platform %q not supported", platform)
			}

			skupperCommand.NewClient(cmd, args)
			return nil
		},
		Run: func(cmd *cobra.Command, args []string) {
			utils.HandleError(utils.ValidationError, skupperCommand.ValidateInput(args))
			skupperCommand.InputToOptions()
			utils.HandleError(utils.GenericError, skupperCommand.Run())
		},
		PostRun: func(cmd *cobra.Command, args []string) {
			utils.HandleError(utils.GenericError, skupperCommand.WaitUntil())
		},
	}

	return &cmd
}
