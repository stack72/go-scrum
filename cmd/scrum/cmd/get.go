package cmd

import (
	"context"
	"fmt"
	"io/ioutil"
	"path"
	"time"

	"github.com/joyent/triton-go/storage"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var getCmd = &cobra.Command{
	Use:          "get",
	Short:        "Get scrum information",
	Long:         `Get scrum information, either for yourself or teammates`,
	SilenceUsage: true,
	Example: `  $ scrum get                      # Get my scrum for today
  $ scrum get -t -u other.username # Get other.username's scrum for tomorrow`,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if err := checkRequiredFlags(cmd.Flags()); err != nil {
			return errors.Wrap(err, "required flag missing")
		}

		return nil
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := getMantaClient()
		if err != nil {
			return errors.Wrap(err, "unable to create a new manta client")
		}

		// setup time format string to get current date
		scrumDate := time.Now()
		switch {
		case viper.GetBool(configKeyTomorrow):
			scrumDate = scrumDate.AddDate(0, 0, 1)
		}

		objectPath := path.Join("stor", "scrum", scrumDate.Format(scrumDateLayout), getUser())

		output, err := client.Objects().Get(context.Background(), &storage.GetObjectInput{
			ObjectPath: objectPath,
		})
		if err != nil {
			return errors.Wrap(err, "unable to get manta object")
		}
		defer output.ObjectReader.Close()

		body, err := ioutil.ReadAll(output.ObjectReader)
		if err != nil {
			return errors.Wrap(err, "unable to read manta object")
		}

		fmt.Printf("%s", string(body))

		return nil
	},
}

func init() {
	rootCmd.AddCommand(getCmd)
	getCmd.MarkFlagRequired("user")
}
