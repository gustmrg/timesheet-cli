package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gustmrg/timesheet-cli/internal/api"
	"github.com/gustmrg/timesheet-cli/internal/credentials"
	"github.com/gustmrg/timesheet-cli/internal/webparse"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type entryFlags struct {
	customer, project, category   string
	date, start, end, description string
	notMonetize, monetize         bool
}

type metaCategory struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}
type metaProject struct {
	ID         int64          `json:"id"`
	Name       string         `json:"name"`
	Categories []metaCategory `json:"categories"`
}
type metaCustomer struct {
	ID       int64         `json:"id"`
	Name     string        `json:"name"`
	Projects []metaProject `json:"projects"`
}

type normalizedEntry struct {
	ID           int64   `json:"id"`
	Customer     api.Ref `json:"customer"`
	Project      api.Ref `json:"project"`
	Category     api.Ref `json:"category"`
	Date         string  `json:"date"`
	Start        string  `json:"start"`
	End          string  `json:"end"`
	Description  string  `json:"description"`
	NotMonetized bool    `json:"notMonetized"`
}

func (a *app) versionCommand() *cobra.Command {
	return &cobra.Command{Use: "version", Short: "Print the version", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		return a.success(map[string]string{"version": a.version}, func() { fmt.Fprintln(a.out, a.version) })
	}}
}

func (a *app) loginCommand() *cobra.Command {
	var user, password, credentialStore string
	var saveCredentials bool
	cmd := &cobra.Command{Use: "login", Short: "Authenticate and save a session", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		if credentialStore != "system" && credentialStore != "file" {
			return fail("invalid_input", "credential store must be `system` or `file`", 2, nil)
		}
		if !saveCredentials && credentialStore != "system" {
			return fail("invalid_input", "--credential-store requires --save-credentials", 2, nil)
		}
		if user == "" {
			user = os.Getenv("TIMESHEET_USER")
		}
		if password == "" {
			password = os.Getenv("TIMESHEET_PASS")
		}
		var err error
		if user == "" {
			user, err = a.ask("Login: ")
			if err != nil {
				return err
			}
		}
		if password == "" {
			password, err = a.askPassword("Password: ")
			if err != nil {
				return err
			}
		}
		client, clientErr := a.client()
		if clientErr != nil {
			return clientErr
		}
		if err := client.Login(user, password); err != nil {
			return classifyAPI(err)
		}
		session, sessionErr := a.sessionFile()
		if sessionErr != nil {
			return sessionErr
		}
		credentialsSaved := false
		credentialLocation := ""
		if saveCredentials {
			var store credentials.Store = a.credentialStore
			if credentialStore == "file" {
				fileStore, path, fileErr := a.fileCredentialStore()
				if fileErr != nil {
					return fileErr
				}
				store = fileStore
				credentialLocation = path
			}
			record := credentials.Record{Username: user, Password: password}
			if err := store.Set(a.baseURL(), record); err != nil {
				location := "the operating system vault"
				if credentialStore == "file" {
					location = "the protected credentials file"
				}
				a.addWarning("credential_store_unavailable", "logged in, but could not save credentials in "+location+"; automatic reauthentication is unavailable")
			} else {
				credentialsSaved = true
			}
		}
		data := map[string]any{"authenticated": true, "sessionFile": session, "credentialsSaved": credentialsSaved}
		if credentialsSaved {
			data["credentialStore"] = credentialStore
		}
		if credentialLocation != "" {
			data["credentialsFile"] = credentialLocation
		}
		return a.success(data, func() {
			fmt.Fprintf(a.out, "logged in — session saved to %s\n", session)
			if credentialsSaved && credentialStore == "file" {
				fmt.Fprintf(a.out, "credentials saved to %s (protected by file permissions)\n", credentialLocation)
			} else if credentialsSaved {
				fmt.Fprintln(a.out, "credentials saved in the operating system vault")
			}
		})
	}}
	cmd.Flags().StringVar(&user, "user", "", "login username")
	cmd.Flags().StringVar(&password, "pass", "", "login password (may be visible in process listings)")
	cmd.Flags().BoolVar(&saveCredentials, "save-credentials", false, "save credentials for automatic reauthentication")
	cmd.Flags().StringVar(&credentialStore, "credential-store", "system", "credential store: system or file")
	return cmd
}

func (a *app) logoutCommand() *cobra.Command {
	var forgetCredentials bool
	cmd := &cobra.Command{Use: "logout", Short: "Remove the saved session", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		session, err := a.sessionFile()
		if err != nil {
			return err
		}
		sessionRemoved := false
		if removeErr := os.Remove(session); removeErr == nil {
			sessionRemoved = true
		} else if !errors.Is(removeErr, os.ErrNotExist) {
			return fail("internal_error", "remove saved session", 1, removeErr)
		}
		credentialsForgotten := false
		if forgetCredentials {
			systemDeleteErr := a.credentialStore.Delete(a.baseURL())
			fileStore, _, fileStoreErr := a.fileCredentialStore()
			if fileStoreErr != nil {
				return fileStoreErr
			}
			fileDeleteErr := fileStore.Delete(a.baseURL())
			if systemDeleteErr != nil && !credentials.IsKind(systemDeleteErr, credentials.KindUnavailable) && !credentials.IsKind(systemDeleteErr, credentials.KindUnsupported) {
				return fail("credential_store_error", "session cleared, but saved credentials may remain in the operating system vault", 1, systemDeleteErr)
			}
			if fileDeleteErr != nil {
				return fail("credential_store_error", "session cleared, but saved credentials may remain in the credentials file", 1, fileDeleteErr)
			}
			credentialsForgotten = true
		}
		data := map[string]any{
			"loggedOut":            true,
			"sessionRemoved":       sessionRemoved,
			"credentialsForgotten": credentialsForgotten,
		}
		return a.success(data, func() {
			fmt.Fprintln(a.out, "logged out")
			if credentialsForgotten {
				fmt.Fprintln(a.out, "saved credentials removed from the operating system vault")
			}
		})
	}}
	cmd.Flags().BoolVar(&forgetCredentials, "forget-credentials", false, "also remove saved credentials from the operating system vault")
	return cmd
}

func (a *app) listCommand() *cobra.Command {
	var all bool
	var limit int
	cmd := &cobra.Command{Use: "list", Short: "List timesheet entries", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		if limit <= 0 {
			return fail("invalid_input", "--limit must be a positive integer", 2, nil)
		}
		client, err := a.client()
		if err != nil {
			return err
		}
		entries, apiErr := client.ListEntries()
		if apiErr != nil {
			return classifyAPI(apiErr)
		}
		total := len(entries)
		if !all && len(entries) > limit {
			entries = entries[:limit]
		}
		data := map[string]any{"entries": entries, "returned": len(entries), "total": total}
		return a.success(data, func() {
			printEntryTable(a.out, entries)
			if len(entries) < total {
				fmt.Fprintf(a.out, "… %d more (use --all)\n", total-len(entries))
			}
		})
	}}
	cmd.Flags().BoolVar(&all, "all", false, "show every entry")
	cmd.Flags().IntVar(&limit, "limit", 20, "maximum number of entries")
	return cmd
}

func (a *app) metaCommand() *cobra.Command {
	return &cobra.Command{Use: "meta", Short: "List customers, projects, and categories", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		client, err := a.client()
		if err != nil {
			return err
		}
		customers, apiErr := loadMeta(client)
		if apiErr != nil {
			return classifyAPI(apiErr)
		}
		return a.success(map[string]any{"customers": customers}, func() { printMeta(a.out, customers) })
	}}
}

func (a *app) statusCommand() *cobra.Command {
	return &cobra.Command{Use: "status <entry-id>", Short: "Show an entry's evaluation status", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		id, err := positiveID(args[0])
		if err != nil {
			return err
		}
		client, clientErr := a.client()
		if clientErr != nil {
			return clientErr
		}
		entries, apiErr := client.ListEntries()
		if apiErr != nil {
			return classifyAPI(apiErr)
		}
		var entry *webparse.Entry
		for index := range entries {
			if entries[index].ID == id {
				entry = &entries[index]
				break
			}
		}
		if entry == nil {
			return fail("not_found", fmt.Sprintf("entry %d not found", id), 5, nil)
		}
		if entry.EvaluateID == nil {
			return fail("not_found", fmt.Sprintf("entry %d has no evaluation", id), 5, nil)
		}
		evaluation, apiErr := client.Evaluate(id, *entry.EvaluateID)
		if apiErr != nil {
			return classifyAPI(apiErr)
		}
		state := evaluationState(evaluation)
		data := map[string]any{"entryId": id, "state": state, "manager": evaluation.ManagerName, "created": evaluation.Created}
		return a.success(data, func() {
			fmt.Fprintf(a.out, "entry %d (%s %s–%s, %s)\n", id, entry.Date, entry.Start, entry.End, entry.Project)
			fmt.Fprintf(a.out, "status:  %s\nmanager: %s\ncreated: %s\n", state, pointerText(evaluation.ManagerName), pointerText(evaluation.Created))
		})
	}}
}

func (a *app) addCommand() *cobra.Command {
	var flags entryFlags
	cmd := &cobra.Command{Use: "add", Short: "Create a timesheet entry", Args: cobra.NoArgs, RunE: func(_ *cobra.Command, _ []string) error {
		if flags.monetize {
			return fail("invalid_input", "--monetize is only meaningful when updating an entry", 2, nil)
		}
		if err := validateEntrySyntax(flags, true); err != nil {
			return err
		}
		client, err := a.client()
		if err != nil {
			return err
		}
		entry, normalized, resolveErr := resolveEntry(client, flags, nil)
		if resolveErr != nil {
			return resolveErr
		}
		created, apiErr := client.Create(entry)
		if apiErr != nil {
			return a.classifyMutationError(client, apiErr)
		}
		normalized.ID = created.ID
		return a.success(map[string]any{"action": "created", "entry": normalized}, func() {
			fmt.Fprintf(a.out, "created entry %d (%s %s–%s, %s)\n", created.ID, entry.Date, entry.Start, entry.End, entry.Project.Name)
		})
	}}
	bindEntryFlags(cmd, &flags)
	return cmd
}

func (a *app) updateCommand() *cobra.Command {
	var flags entryFlags
	cmd := &cobra.Command{Use: "update <entry-id>", Short: "Update a timesheet entry", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if flags.monetize && flags.notMonetize {
			return fail("invalid_input", "--monetize and --not-monetize are mutually exclusive", 2, nil)
		}
		if cmd.Flags().Changed("desc") && flags.description == "" {
			return fail("invalid_input", "--desc cannot be empty", 2, nil)
		}
		id, err := positiveID(args[0])
		if err != nil {
			return err
		}
		if err := validateEntrySyntax(flags, false); err != nil {
			return err
		}
		client, clientErr := a.client()
		if clientErr != nil {
			return clientErr
		}
		current, apiErr := client.Worksheet(id)
		if apiErr != nil {
			return classifyAPI(apiErr)
		}
		if current.ID == 0 {
			return fail("not_found", fmt.Sprintf("entry %d not found", id), 5, nil)
		}
		base := &entryBase{customer: api.Ref{ID: current.IDCustomer}, project: api.Ref{ID: current.IDProject}, category: api.Ref{ID: current.IDCategory}, date: current.InformedDate, start: current.StartTime, end: current.EndTime, description: current.Description, descriptionHTML: !cmd.Flags().Changed("desc"), notMonetize: current.NotMonetize}
		if cmd.Flags().Changed("not-monetize") {
			base.notMonetize = true
		}
		if cmd.Flags().Changed("monetize") {
			base.notMonetize = false
		}
		entry, normalized, resolveErr := resolveEntry(client, flags, base)
		if resolveErr != nil {
			return resolveErr
		}
		if apiErr := client.Update(id, entry); apiErr != nil {
			return a.classifyMutationError(client, apiErr)
		}
		normalized.ID = id
		return a.success(map[string]any{"action": "updated", "entry": normalized}, func() {
			fmt.Fprintf(a.out, "updated entry %d (%s %s–%s, %s)\n", id, entry.Date, entry.Start, entry.End, entry.Project.Name)
		})
	}}
	bindEntryFlags(cmd, &flags)
	cmd.Flags().BoolVar(&flags.monetize, "monetize", false, "mark the entry as monetized")
	return cmd
}

func (a *app) deleteCommand() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{Use: "delete <entry-id>", Short: "Delete a timesheet entry", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		id, err := positiveID(args[0])
		if err != nil {
			return err
		}
		client, clientErr := a.client()
		if clientErr != nil {
			return clientErr
		}
		if !yes {
			entries, apiErr := client.ListEntries()
			if apiErr != nil {
				return classifyAPI(apiErr)
			}
			what := fmt.Sprintf("id %d", id)
			for _, entry := range entries {
				if entry.ID == id {
					what = fmt.Sprintf("%s %s–%s %s (%s)", entry.Date, entry.Start, entry.End, entry.Project, entry.Category)
					break
				}
			}
			fmt.Fprintf(a.promptWriter(), "Delete entry %s? [y/N] ", what)
			answer, readErr := bufio.NewReader(a.in).ReadString('\n')
			if readErr != nil && readErr != io.EOF {
				return fail("internal_error", "read confirmation", 1, readErr)
			}
			if !yesAnswer(answer) {
				return a.success(map[string]any{"id": id, "deleted": false}, func() { fmt.Fprintln(a.out, "aborted") })
			}
		}
		if apiErr := client.Delete(id); apiErr != nil {
			return a.classifyMutationError(client, apiErr)
		}
		return a.success(map[string]any{"id": id, "deleted": true}, func() { fmt.Fprintf(a.out, "deleted entry %d\n", id) })
	}}
	cmd.Flags().BoolVar(&yes, "yes", false, "delete without prompting")
	return cmd
}

func (a *app) classifyMutationError(client *api.Client, err error) error {
	if !api.IsKind(err, api.KindAuth) {
		return classifyAPI(err)
	}
	if renewErr := client.Renew(); renewErr != nil {
		return classifyAPI(renewErr)
	}
	return fail("auth_required", "session renewed; rerun the command to confirm the write", 3, err)
}

type entryBase struct {
	customer, project, category   api.Ref
	date, start, end, description string
	descriptionHTML, notMonetize  bool
}

func resolveEntry(client *api.Client, flags entryFlags, base *entryBase) (api.EntryInput, normalizedEntry, error) {
	metadata, err := client.Metadata()
	if err != nil {
		return api.EntryInput{}, normalizedEntry{}, classifyAPI(err)
	}
	customers := refs(metadata["customer"])
	var baseCustomer *api.Ref
	if base != nil {
		baseCustomer = &base.customer
	}
	customer, pickErr := pick("customer", first(flags.customer, refID(baseCustomer)), customers, baseCustomer)
	if pickErr != nil {
		return api.EntryInput{}, normalizedEntry{}, pickErr
	}
	projectRows, err := client.DropDownChange(api.DropDownQuery{CustomerID: customer.ID})
	if err != nil {
		return api.EntryInput{}, normalizedEntry{}, classifyAPI(err)
	}
	projects := projectRefs(projectRows)
	var baseProject *api.Ref
	if base != nil {
		baseProject = &base.project
	}
	project, pickErr := pick("project", first(flags.project, refID(baseProject)), projects, baseProject)
	if pickErr != nil {
		return api.EntryInput{}, normalizedEntry{}, pickErr
	}
	categoryRows, err := client.DropDownChange(api.DropDownQuery{ProjectID: project.ID})
	if err != nil {
		return api.EntryInput{}, normalizedEntry{}, classifyAPI(err)
	}
	categories := categoryRefs(categoryRows)
	var baseCategory *api.Ref
	if base != nil {
		baseCategory = &base.category
	}
	category, pickErr := pick("category", first(flags.category, refID(baseCategory)), categories, baseCategory)
	if pickErr != nil {
		return api.EntryInput{}, normalizedEntry{}, pickErr
	}

	date := flags.date
	if date == "" && base != nil {
		date = base.date
	}
	if date == "" {
		date = time.Now().Format("2006-01-02")
	}
	wireDate, dateErr := parseDate(date)
	if dateErr != nil {
		return api.EntryInput{}, normalizedEntry{}, dateErr
	}
	start := flags.start
	if start == "" && base != nil {
		start = base.start
	}
	end := flags.end
	if end == "" && base != nil {
		end = base.end
	}
	if err := validateTime("start", start); err != nil {
		return api.EntryInput{}, normalizedEntry{}, err
	}
	if err := validateTime("end", end); err != nil {
		return api.EntryInput{}, normalizedEntry{}, err
	}
	description := flags.description
	descriptionHTML := false
	if description == "" && base != nil {
		description = base.description
		descriptionHTML = base.descriptionHTML
	}
	if description == "" {
		return api.EntryInput{}, normalizedEntry{}, fail("invalid_input", "missing --desc", 2, nil)
	}
	notMonetize := flags.notMonetize
	if base != nil {
		notMonetize = base.notMonetize
	}
	input := api.EntryInput{Customer: customer, Project: project, Category: category, Date: wireDate, Start: start, End: end, Description: description, DescriptionHTML: descriptionHTML, NotMonetize: notMonetize}
	normalizedDescription := description
	if descriptionHTML {
		if text, err := webparse.FragmentText(description); err == nil {
			normalizedDescription = text
		}
	}
	normalized := normalizedEntry{Customer: customer, Project: project, Category: category, Date: wireDate, Start: start, End: end, Description: normalizedDescription, NotMonetized: notMonetize}
	return input, normalized, nil
}

func bindEntryFlags(cmd *cobra.Command, flags *entryFlags) {
	cmd.Flags().StringVar(&flags.customer, "customer", "", "customer ID or unique name substring")
	cmd.Flags().StringVar(&flags.project, "project", "", "project ID or unique name substring")
	cmd.Flags().StringVar(&flags.category, "category", "", "category ID or unique name substring")
	cmd.Flags().StringVar(&flags.date, "date", "", "date in YYYY-MM-DD or dd/MM/yyyy format")
	cmd.Flags().StringVar(&flags.start, "start", "", "start time in HH:mm format")
	cmd.Flags().StringVar(&flags.end, "end", "", "end time in HH:mm format")
	cmd.Flags().StringVar(&flags.description, "desc", "", "plain-text description")
	cmd.Flags().BoolVar(&flags.notMonetize, "not-monetize", false, "mark the entry as not monetized")
}

func loadMeta(client *api.Client) ([]metaCustomer, error) {
	metadata, err := client.Metadata()
	if err != nil {
		return nil, err
	}
	customers := make([]metaCustomer, 0, len(metadata["customer"]))
	for _, customer := range metadata["customer"] {
		projectRows, err := client.DropDownChange(api.DropDownQuery{CustomerID: customer.ID})
		if err != nil {
			return nil, err
		}
		projects := make([]metaProject, 0)
		seenProjects := map[int64]bool{}
		for _, row := range projectRows {
			if row.IDProject == 0 || seenProjects[row.IDProject] {
				continue
			}
			seenProjects[row.IDProject] = true
			categoryRows, err := client.DropDownChange(api.DropDownQuery{ProjectID: row.IDProject})
			if err != nil {
				return nil, err
			}
			categories := make([]metaCategory, 0)
			seenCategories := map[int64]bool{}
			for _, category := range categoryRows {
				if category.IDCategory != 0 && !seenCategories[category.IDCategory] {
					seenCategories[category.IDCategory] = true
					categories = append(categories, metaCategory{ID: category.IDCategory, Name: category.CategoryName})
				}
			}
			projects = append(projects, metaProject{ID: row.IDProject, Name: row.ProjectName, Categories: categories})
		}
		customers = append(customers, metaCustomer{ID: customer.ID, Name: customer.Name, Projects: projects})
	}
	return customers, nil
}

func refs(options []webparse.Option) []api.Ref {
	result := make([]api.Ref, len(options))
	for i, option := range options {
		result[i] = api.Ref{ID: option.ID, Name: option.Name}
	}
	return result
}
func projectRefs(rows []api.DropDownRecord) []api.Ref {
	return uniqueRefs(rows, func(row api.DropDownRecord) api.Ref { return api.Ref{ID: row.IDProject, Name: row.ProjectName} })
}
func categoryRefs(rows []api.DropDownRecord) []api.Ref {
	return uniqueRefs(rows, func(row api.DropDownRecord) api.Ref { return api.Ref{ID: row.IDCategory, Name: row.CategoryName} })
}
func uniqueRefs(rows []api.DropDownRecord, convert func(api.DropDownRecord) api.Ref) []api.Ref {
	var result []api.Ref
	seen := map[int64]bool{}
	for _, row := range rows {
		ref := convert(row)
		if ref.ID != 0 && !seen[ref.ID] {
			seen[ref.ID] = true
			result = append(result, ref)
		}
	}
	return result
}

func pick(kind, wanted string, options []api.Ref, fallback *api.Ref) (api.Ref, error) {
	if wanted == "" {
		return api.Ref{}, fail("invalid_input", "missing --"+kind, 2, nil)
	}
	for _, option := range options {
		if strconv.FormatInt(option.ID, 10) == wanted {
			return option, nil
		}
	}
	lower := strings.ToLower(wanted)
	var matches []api.Ref
	for _, option := range options {
		if strings.Contains(strings.ToLower(option.Name), lower) {
			matches = append(matches, option)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		names := make([]string, len(matches))
		for i := range matches {
			names[i] = matches[i].Name
		}
		return api.Ref{}, fail("ambiguous_value", fmt.Sprintf("--%s %q is ambiguous: %s", kind, wanted, strings.Join(names, ", ")), 2, nil)
	}
	if fallback != nil && strconv.FormatInt(fallback.ID, 10) == wanted && fallback.Name != "" {
		return *fallback, nil
	}
	return api.Ref{}, fail("invalid_input", fmt.Sprintf("unknown %s: %q (see `timesheet meta`)", kind, wanted), 2, nil)
}

func positiveID(value string) (int64, error) {
	id, err := strconv.ParseInt(value, 10, 64)
	if err != nil || id <= 0 {
		return 0, fail("invalid_input", fmt.Sprintf("invalid entry ID %q", value), 2, err)
	}
	return id, nil
}

func parseDate(value string) (string, error) {
	for _, layout := range []string{"2006-01-02", "02/01/2006"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format("02/01/2006"), nil
		}
	}
	return "", fail("invalid_input", fmt.Sprintf("invalid date %q — use YYYY-MM-DD or dd/MM/yyyy", value), 2, nil)
}

func validateTime(kind, value string) error {
	if value == "" {
		return fail("invalid_input", "missing --"+kind, 2, nil)
	}
	parsed, err := time.Parse("15:04", value)
	if err != nil || parsed.Format("15:04") != value {
		return fail("invalid_input", fmt.Sprintf("invalid %s time %q — use HH:mm", kind, value), 2, err)
	}
	return nil
}

func validateEntrySyntax(flags entryFlags, required bool) error {
	if required {
		for _, field := range []struct{ name, value string }{{"customer", flags.customer}, {"project", flags.project}, {"category", flags.category}, {"desc", flags.description}} {
			if field.value == "" {
				return fail("invalid_input", "missing --"+field.name, 2, nil)
			}
		}
	}
	if flags.date != "" {
		if _, err := parseDate(flags.date); err != nil {
			return err
		}
	}
	if flags.start != "" {
		if err := validateTime("start", flags.start); err != nil {
			return err
		}
	} else if required {
		return fail("invalid_input", "missing --start", 2, nil)
	}
	if flags.end != "" {
		if err := validateTime("end", flags.end); err != nil {
			return err
		}
	} else if required {
		return fail("invalid_input", "missing --end", 2, nil)
	}
	return nil
}

func evaluationState(value api.EvaluateInfo) string {
	switch {
	case value.IsApprove == "1":
		return "approved"
	case value.IsReprove == "1":
		return "rejected"
	case value.IsPreApproved == "1":
		return "pre_approved"
	case value.IsPreReproved == "1":
		return "pre_rejected"
	case value.IsReview == "1":
		return "in_review"
	default:
		return "pending"
	}
}
func pointerText(value *string) string {
	if value == nil || *value == "" {
		return "—"
	}
	return *value
}
func yesAnswer(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	return normalized == "y" || normalized == "yes"
}
func first(primary, fallback string) string {
	if primary != "" {
		return primary
	}
	return fallback
}
func refID(ref *api.Ref) string {
	if ref == nil {
		return ""
	}
	return strconv.FormatInt(ref.ID, 10)
}
func (a *app) ask(question string) (string, error) {
	fmt.Fprint(a.promptWriter(), question)
	value, err := bufio.NewReader(a.in).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fail("internal_error", "read input", 1, err)
	}
	return strings.TrimSpace(value), nil
}
func (a *app) askPassword(question string) (string, error) {
	file, ok := a.in.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return "", fail("invalid_input", "no TTY — pass --pass or TIMESHEET_PASS", 2, nil)
	}
	promptOut := a.promptWriter()
	fmt.Fprint(promptOut, question)
	value, err := term.ReadPassword(int(file.Fd()))
	fmt.Fprintln(promptOut)
	if err != nil {
		return "", fail("internal_error", "read password", 1, err)
	}
	return string(value), nil
}

func (a *app) promptWriter() io.Writer {
	if a.json {
		return a.errOut
	}
	return a.out
}

func printEntryTable(w io.Writer, entries []webparse.Entry) {
	head := []string{"ID", "DATE", "TIME", "TOTAL", "STATUS", "PROJECT", "CATEGORY"}
	rows := make([][]string, len(entries))
	for i, entry := range entries {
		rows[i] = []string{strconv.FormatInt(entry.ID, 10), entry.Date, entry.Start + "–" + entry.End, entry.Total, entry.Status, entry.Project, entry.Category}
	}
	widths := make([]int, len(head))
	for i, value := range head {
		widths[i] = len([]rune(value))
	}
	for _, row := range rows {
		for i, value := range row {
			if width := len([]rune(value)); width > widths[i] {
				widths[i] = width
			}
		}
	}
	line := func(values []string) {
		for i, value := range values {
			if i > 0 {
				fmt.Fprint(w, "  ")
			}
			fmt.Fprint(w, value)
			if i < len(values)-1 {
				fmt.Fprint(w, strings.Repeat(" ", widths[i]-len([]rune(value))))
			}
		}
		fmt.Fprintln(w)
	}
	line(head)
	separators := make([]string, len(widths))
	for i, width := range widths {
		separators[i] = strings.Repeat("─", width)
	}
	line(separators)
	for _, row := range rows {
		line(row)
	}
}
func printMeta(w io.Writer, customers []metaCustomer) {
	for _, customer := range customers {
		fmt.Fprintf(w, "%d  %s\n", customer.ID, customer.Name)
		for _, project := range customer.Projects {
			fmt.Fprintf(w, "  %d  %s\n", project.ID, project.Name)
			for _, category := range project.Categories {
				fmt.Fprintf(w, "    %d  %s\n", category.ID, category.Name)
			}
		}
	}
}
