package cmd

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/gwydirsam/go-scrum/pager"
	"github.com/joyent/triton-go/storage"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/ryanuber/columnize"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/crypto/ssh/terminal"
)

func init() {
	{
		const (
			key          = configKeyGetAll
			longName     = "all"
			shortName    = "a"
			defaultValue = false
			description  = "Get scrum for all users"
		)

		getCmd.Flags().BoolP(longName, shortName, defaultValue, description)
		viper.BindPFlag(key, getCmd.Flags().Lookup(longName))
		viper.SetDefault(key, defaultValue)
	}

	{
		const (
			key         = configKeyGetInputDate
			longName    = "date"
			shortName   = "D"
			description = "Date for scrum"
		)
		defaultValue := time.Now().Format(dateInputFormat)

		getCmd.Flags().StringP(longName, shortName, defaultValue, description)
		viper.BindPFlag(key, getCmd.Flags().Lookup(longName))
		viper.SetDefault(key, defaultValue)
	}

	{
		const (
			key               = configKeyGetTomorrow
			longOpt, shortOpt = "tomorrow", "t"
			defaultValue      = false
		)
		getCmd.Flags().BoolP(longOpt, shortOpt, defaultValue, "Get scrum for the next day")
		viper.BindPFlag(key, getCmd.Flags().Lookup(longOpt))
		viper.SetDefault(key, defaultValue)
	}

	{
		const (
			key               = configKeyGetUsername
			longOpt, shortOpt = "user", "u"
			defaultValue      = "$USER"
		)
		getCmd.Flags().StringP(longOpt, shortOpt, defaultValue, "Get scrum for specified user")
		viper.BindPFlag(key, getCmd.Flags().Lookup(longOpt))
		viper.BindEnv(key, "USER")
		viper.SetDefault(key, defaultValue)
	}

	{
		const (
			key          = configKeyGetUsePager
			longName     = "use-pager"
			shortName    = "P"
			defaultValue = true
			description  = "Use a pager to read the output (defaults to $PAGER, less(1), or more(1))"
		)

		getCmd.Flags().BoolP(longName, shortName, defaultValue, description)
		viper.BindPFlag(key, getCmd.Flags().Lookup(longName))
		viper.SetDefault(key, defaultValue)
	}

	{
		const (
			key          = configKeyGetUTC
			longName     = "utc"
			shortName    = "Z"
			defaultValue = false
			description  = "Get mtime data in UTC"
		)

		getCmd.Flags().BoolP(longName, shortName, defaultValue, description)
		viper.BindPFlag(key, getCmd.Flags().Lookup(longName))
		viper.SetDefault(key, defaultValue)
	}

	{
		const (
			key               = configKeyGetYesterday
			longOpt, shortOpt = "yesterday", "y"
			defaultValue      = false
		)
		getCmd.Flags().BoolP(longOpt, shortOpt, defaultValue, "Get scrum for yesterday")
		viper.BindPFlag(key, getCmd.Flags().Lookup(longOpt))
		viper.SetDefault(key, defaultValue)
	}

	rootCmd.AddCommand(getCmd)
}

var getCmd = &cobra.Command{
	Use:          "get",
	SuggestFor:   []string{"fetch", "pull"},
	Short:        "Get scrum information",
	Long:         `Get scrum information, either for yourself (or teammates)`,
	SilenceUsage: true,
	Example: `  $ scrum get                      # Get my scrum for today
  $ scrum get -t -u other.username # Get other.username's scrum for tomorrow`,
	Args: cobra.NoArgs,
	PreRunE: func(cmd *cobra.Command, args []string) error {
		if err := checkRequiredFlags(cmd.Flags()); err != nil {
			return errors.Wrap(err, "required flag missing")
		}

		if viper.GetBool(configKeyGetTomorrow) && viper.GetBool(configKeyGetYesterday) {
			return errors.New("tomorrow and yesterday are conflicting optoins")
		}

		return nil
	},

	RunE: func(cmd *cobra.Command, args []string) error {
		color.NoColor = !viper.GetBool(configKeyLogTermColor)

		client, err := getMantaClient()
		if err != nil {
			return errors.Wrap(err, "unable to create a new manta client")
		}

		scrumDate, err := time.Parse(dateInputFormat, viper.GetString(configKeyGetInputDate))
		if err != nil {
			return errors.Wrap(err, "unable to parse date")
		}

		switch {
		case viper.GetBool(configKeyGetTomorrow):
			scrumDate = scrumDate.AddDate(0, 0, 1)
		case viper.GetBool(configKeyGetYesterday):
			scrumDate = scrumDate.AddDate(0, 0, -1)
		}

		var w io.Writer
		if viper.GetBool(configKeyGetUsePager) {
			p, err := pager.New()
			if err != nil {
				return errors.Wrap(err, "unable to open pager")
			}
			defer pager.Wait()
			w = p
		} else {
			w = os.Stdout
		}

		switch {
		case viper.GetBool(configKeyGetAll):
			return getAllScrum(w, client, scrumDate)
		case !viper.GetBool(configKeyGetAll):
			return getSingleScrum(w, client, scrumDate, getUser(configKeyGetUsername), false)
		default:
			return errors.New("unsupported get mode")
		}
	},
}

func getAllScrum(unbufOut io.Writer, c *storage.StorageClient, scrumDate time.Time) error {
	scrumPath := path.Join("stor", "scrum", scrumDate.Format(scrumDateLayout))

	dirEnts, err := c.Dir().List(context.Background(), &storage.ListDirectoryInput{
		DirectoryName: scrumPath,
	})
	if err != nil {
		return errors.Wrap(err, "unable to list manta directory")
	}

	if dirEnts.ResultSetSize == 0 {
		log.Error().Time("scrum-date", scrumDate).Msg("no users have scrummed for this day")
		return nil
	}

	w := bufio.NewWriter(unbufOut)
	defer w.Flush()

	const defaultTerminalWidth = 80
	terminalWidth, _, err := terminal.GetSize(int(os.Stdin.Fd()))
	if err != nil {
		log.Warn().Err(err).Msg("unable to get terminal size, using default")
		terminalWidth = defaultTerminalWidth
	}

	horizontalSeparator := strings.Repeat("-", terminalWidth) + "\n"

	var firstError error
	for _, ent := range dirEnts.Entries {
		if v, found := usernameActionMap[ent.Name]; found && v == _Ignore {
			continue
		}

		w.WriteString(horizontalSeparator)

		if err := getSingleScrum(w, c, scrumDate, ent.Name, true); err != nil {
			log.Error().Err(err).Str("username", ent.Name).Msg("unable to get user's scrum")
			if firstError == nil {
				firstError = err
			}
		}

		// Paper over slow object fetching in Manta and flush every entry in order
		// to prevent tearing.
		//
		// TODO(seanc@): fetch entries in parallel, pipeline all requests, or figure
		// out how to do a multi-GET.
		w.Flush()
	}

	if firstError != nil {
		return firstError
	}

	return nil
}

func getSingleScrum(w io.Writer, c *storage.StorageClient, scrumDate time.Time, user string, includeHeader bool) error {
	objectPath := path.Join("stor", "scrum", scrumDate.Format(scrumDateLayout), user)

	obj, err := c.Objects().Get(context.Background(), &storage.GetObjectInput{
		ObjectPath: objectPath,
	})
	if err != nil {
		return errors.Wrap(err, "unable to get manta object")
	}
	defer obj.ObjectReader.Close()

	body, err := ioutil.ReadAll(obj.ObjectReader)
	if err != nil {
		return errors.Wrap(err, "unable to read manta object")
	}

	if includeHeader {
		keyFmt := color.New(color.FgHiWhite, color.Bold).SprintFunc()
		userFmt := color.New(color.FgHiWhite, color.Underline).SprintFunc()
		mtimeFmt := color.New().SprintFunc()

		var mtime time.Time
		if viper.GetBool(configKeyGetUTC) {
			mtime = obj.LastModified.UTC()
		} else {
			mtime = obj.LastModified.Local()
		}

		output := []string{
			fmt.Sprintf("%s | %s", keyFmt("user"), userFmt(user)),
			fmt.Sprintf("%s | %s", keyFmt("mtime"), mtimeFmt(mtime.Format(mtimeFormatTZ))),
		}
		w.Write([]byte(columnize.SimpleFormat(output) + "\n\n"))
	}

	w.Write(bytes.TrimSpace(body))
	w.Write([]byte("\n"))

	return nil
}
