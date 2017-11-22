package cmd

import (
	"io"
	"log"
	"os"
	"path"
	"strings"
	"time"

	manta "github.com/jen20/manta-go"
	"github.com/jen20/manta-go/authentication"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	iFile       string
	force       bool
	tomorrow    bool
	ndays       int
	numSick     int
	numVacation int
)

var setCmd = &cobra.Command{
	Use:   "set",
	Short: "Set your scrum status",
	Long:  `Set your scrum status`,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		return CheckRequiredFlags(cmd.Flags())
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		//// validate username
		//json, err := ioutil.ReadFile("team.json")
		//if err != nil {
		//	log.Fatalf("ioutil.ReadFile: %s", err)
		//}
		//result := gjson.GetBytes(json, userName)
		//if result.String() == "" {
		//	log.Fatalf("Expected Engineer")
		//}

		// setup account
		account := "Joyent_Dev"
		mantaURL = viper.GetString("manta_url")
		mantaKeyId = viper.GetString("manta_key_id")

		// setup client
		sshKeySigner, err := authentication.NewSSHAgentSigner(
			mantaKeyId, account)
		if err != nil {
			return errors.Wrap(err, "unable to create a new SSH signer")
		}

		client, err := manta.NewClient(&manta.ClientOptions{
			Endpoint:    mantaURL,
			AccountName: account,
			Signers:     []authentication.Signer{sshKeySigner},
		})
		if err != nil {
			return errors.Wrap(err, "unable to create a new manta client")
		}

		// Build file string
		// setup time format string to get current date
		layout := "2006/01/02"
		scrumDate := time.Now()
		if tomorrow {
			scrumDate = scrumDate.AddDate(0, 0, 1)
		} else if ndays != 0 {
			scrumDate = scrumDate.AddDate(0, 0, ndays)
		}

		// create end date string for vacation and sick time
		endDate := time.Now()
		daysToScrum := 1
		if numSick > 0 || numVacation > 0 {
			daysToScrum = max(numSick, numVacation)
			endDate = endDate.AddDate(0, 0, daysToScrum)
		}

		for i := daysToScrum; i > 0; i-- {
			scrumPath := "scrum/" + scrumDate.Format(layout) + "/" + userName

			// Check if scrum exists
			_, err = client.GetObject(&manta.GetObjectInput{
				ObjectPath: scrumPath,
			})

			switch {
			case err != nil && manta.IsDirectoryDoesNotExistError(err):
				dirs := strings.Split(scrumDate.Format(scrumDateLayout), "/")
				createDir := "scrum/"
				for _, dir := range dirs {
					createDir += dir + "/"
					err = client.PutDirectory(&manta.PutDirectoryInput{
						DirectoryName: createDir,
					})
					if err != nil {
						return errors.Wrap(err, "unable to put object")
					}
				}
			case err != nil && !manta.IsResourceNotFoundError(err):
				return errors.Wrap(err, "unable to get object")
			case !force:
				// if not, we need a force flag
				return errors.Wrapf(err, "~~/stor/%s exists and -f not specified", scrumPath)
			case err == nil:
				log.Printf("scrum for %q already exists, specify -f to override", scrumPath)
				continue
			}

			log.Printf("scrum: scrumming for %s", scrumDate.Format(layout))
			var reader io.ReadSeeker
			if numSick != 0 {
				reader = strings.NewReader("Sick leave until " + endDate.Format(layout) + "\n")
			} else if numVacation != 0 {
				reader = strings.NewReader("Vacation until " + endDate.Format(layout) + "\n")
			} else if iFile != "" {
				f, err := os.Open(iFile)
				if err != nil {
					return errors.Wrap(err, "unable to open file")
				}
				defer f.Close()
				reader = f
			}

			if err := putObject(client, scrumPath, reader); err != nil {
				return errors.Wrap(err, "unable to put object")
			}

			// scrum for next day
			scrumDate = scrumDate.AddDate(0, 0, 1)
		}

		return nil
	},
}

func init() {
	RootCmd.AddCommand(setCmd)

	setCmd.Flags().BoolVarP(&force, "force", "f", false, "Force overwrite of any present scrum")
	setCmd.Flags().BoolVarP(&tomorrow, "tomorrow", "t", false, "Scrum for tomorrow")

	setCmd.Flags().IntVarP(&ndays, "days", "d", 0, "Scrum for n days from now")
	setCmd.Flags().IntVarP(&numSick, "sick", "s", 0, "Sick leave for n days")
	setCmd.Flags().IntVarP(&numVacation, "vacation", "v", 0, "Vacation for n days")

	setCmd.Flags().StringVarP(&iFile, "file", "i", "", "file to read scrum from")

	// Required Flags

	setCmd.Flags().StringVarP(&userName, "user", "u", "", "user to scrum as")
	setCmd.MarkFlagRequired("user")
}

func max(a, b int) int {
	if a < b {
		return b
	}
	return a
}

func putObject(client *manta.Client, scrumPath string, reader io.ReadSeeker) error {
	err := client.PutObject(&manta.PutObjectInput{
		ObjectPath:   scrumPath,
		ObjectReader: reader,
	})
	if err != nil {
		return errors.Wrap(err, "unable to put object")
	}
	log.Printf("scrum: got it")

	return nil
}
