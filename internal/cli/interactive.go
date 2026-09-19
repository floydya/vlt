package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"charm.land/huh/v2"

	"vlt/internal/profile"
)

var (
	errProfileSelectionRequiresTerminal = errors.New("profile selection is required outside an interactive terminal")
	errNoProfilesConfigured             = errors.New("no profiles configured")
)

type ProfileSelector interface {
	Select(context.Context, []string, string) (string, error)
}

type ProfileFormRequest struct {
	Profile      profile.Profile
	NameEditable bool
}

type ProfileForm interface {
	Run(context.Context, ProfileFormRequest) (profile.Profile, error)
}

type ProfileRemovalConfirmation struct {
	Name                  string
	LeavesNoActiveProfile bool
}

type ProfileRemovalConfirmer interface {
	Confirm(context.Context, ProfileRemovalConfirmation) (bool, error)
}

type huhProfileSelector struct {
	input      io.Reader
	output     io.Writer
	accessible bool
}

type huhProfileForm struct {
	input      io.Reader
	output     io.Writer
	accessible bool
}

type huhProfileRemovalConfirmer struct {
	input      io.Reader
	output     io.Writer
	accessible bool
}

func NewHuhProfileSelector(input io.Reader, output io.Writer) ProfileSelector {
	return huhProfileSelector{input: input, output: output}
}

func NewHuhProfileForm(input io.Reader, output io.Writer) ProfileForm {
	return huhProfileForm{input: input, output: output}
}

func NewHuhProfileRemovalConfirmer(input io.Reader, output io.Writer) ProfileRemovalConfirmer {
	return huhProfileRemovalConfirmer{input: input, output: output}
}

func (s huhProfileSelector) Select(ctx context.Context, names []string, active string) (string, error) {
	if len(names) == 0 {
		return "", errors.New("select profile: no profile choices")
	}
	if s.input == nil {
		return "", errors.New("select profile: input is not configured")
	}
	if s.output == nil {
		return "", errors.New("select profile: output is not configured")
	}

	selected := names[0]
	options := make([]huh.Option[string], 0, len(names))
	for _, name := range names {
		label := name
		isActive := name == active
		if isActive {
			label += " (active)"
			selected = name
		}
		options = append(options, huh.NewOption(label, name).Selected(isActive))
	}

	field := huh.NewSelect[string]().
		Title("Select a profile").
		Options(options...).
		Value(&selected)
	form := huh.NewForm(huh.NewGroup(field)).
		WithInput(s.input).
		WithOutput(s.output).
		WithAccessible(s.accessible)
	if err := form.RunWithContext(ctx); err != nil {
		return "", fmt.Errorf("select profile: %w", err)
	}
	return selected, nil
}

func (f huhProfileForm) Run(ctx context.Context, request ProfileFormRequest) (profile.Profile, error) {
	if f.input == nil {
		return profile.Profile{}, errors.New("profile form: input is not configured")
	}
	if f.output == nil {
		return profile.Profile{}, errors.New("profile form: output is not configured")
	}
	candidate := request.Profile
	if candidate.AuthPath == "" {
		candidate.AuthPath = "oidc"
	}

	fields := make([]huh.Field, 0, 5)
	if request.NameEditable {
		fields = append(fields,
			huh.NewInput().Title("Name").Value(&candidate.Name).Validate(profileFormInputValidator(candidate.Name, f.accessible, profile.ValidateName)),
		)
	}
	fields = append(fields,
		huh.NewInput().Title("Address").Value(&candidate.Address).Validate(profileFormInputValidator(candidate.Address, f.accessible, profile.ValidateAddress)),
		huh.NewInput().Title("Username").Value(&candidate.Username).Validate(profileFormValidator(candidate.Username, f.accessible, func(candidate *profile.Profile, value string) {
			candidate.Username = value
		})),
		huh.NewInput().Title("Auth path").Value(&candidate.AuthPath).Validate(profileFormValidator(candidate.AuthPath, f.accessible, func(candidate *profile.Profile, value string) {
			candidate.AuthPath = value
		})),
		huh.NewInput().Title("Namespace").Value(&candidate.Namespace).Validate(profileFormValidator(candidate.Namespace, f.accessible, func(candidate *profile.Profile, value string) {
			candidate.Namespace = value
		})),
	)
	form := huh.NewForm(huh.NewGroup(fields...).Title("Profile details")).
		WithInput(f.input).
		WithOutput(f.output).
		WithAccessible(f.accessible)
	if err := form.RunWithContext(ctx); err != nil {
		return profile.Profile{}, fmt.Errorf("profile form: %w", err)
	}
	if err := candidate.Validate(); err != nil {
		return profile.Profile{}, fmt.Errorf("profile form: validate result: %w", err)
	}
	return candidate, nil
}

func (c huhProfileRemovalConfirmer) Confirm(ctx context.Context, request ProfileRemovalConfirmation) (bool, error) {
	if c.input == nil {
		return false, errors.New("confirm profile removal: input is not configured")
	}
	if c.output == nil {
		return false, errors.New("confirm profile removal: output is not configured")
	}

	title := fmt.Sprintf("Remove profile %q and its stored credential?", request.Name)
	if request.LeavesNoActiveProfile {
		title += " This will leave no active profile."
	}
	confirmed := false
	field := huh.NewConfirm().
		Title(title).
		Affirmative("Remove").
		Negative("Keep").
		Value(&confirmed)
	form := huh.NewForm(huh.NewGroup(field)).
		WithInput(c.input).
		WithOutput(c.output).
		WithAccessible(c.accessible)
	if err := form.RunWithContext(ctx); err != nil {
		return false, fmt.Errorf("confirm profile removal: %w", err)
	}
	return confirmed, nil
}

func profileFormInputValidator(defaultValue string, allowDefault bool, validate func(string) error) func(string) error {
	return func(value string) error {
		if allowDefault && strings.TrimSpace(value) == "" && defaultValue != "" {
			return nil
		}
		return validate(value)
	}
}

func profileFormValidator(defaultValue string, allowDefault bool, setValue func(*profile.Profile, string)) func(string) error {
	return func(value string) error {
		if allowDefault && strings.TrimSpace(value) == "" && defaultValue != "" {
			return nil
		}
		candidate := profile.Profile{
			Name: "profile", Address: "https://vault.example.com", Username: "user", AuthPath: "oidc",
		}
		setValue(&candidate, value)
		return candidate.Validate()
	}
}

func selectProfileInteractively(
	ctx context.Context,
	profiles ConfigurationLoader,
	terminal Terminal,
	selector ProfileSelector,
) (profile.Profile, string, error) {
	if err := requireInteractiveProfileTerminal(terminal); err != nil {
		return profile.Profile{}, "", err
	}
	if profiles == nil {
		return profile.Profile{}, "", errors.New("profile selection: profile configuration is not configured")
	}
	configuration, err := profiles.Load(ctx)
	if err != nil {
		return profile.Profile{}, "", fmt.Errorf("profile selection: load profiles: %w", err)
	}
	service := profile.NewService(configuration.Profiles)
	candidates := service.List()
	if len(candidates) == 0 {
		return profile.Profile{}, "", errNoProfilesConfigured
	}
	if selector == nil {
		return profile.Profile{}, "", errors.New("profile selection: interactive selector is not configured")
	}

	names := make([]string, len(candidates))
	for index, candidate := range candidates {
		names[index] = candidate.Name
	}
	selectedName, err := selector.Select(ctx, names, configuration.ActiveProfile)
	if err != nil {
		return profile.Profile{}, "", fmt.Errorf("profile selection: %w", err)
	}
	selected, err := service.Find(selectedName)
	if err != nil {
		return profile.Profile{}, "", fmt.Errorf("profile selection: resolve selected profile: %w", err)
	}
	return selected, configuration.ActiveProfile, nil
}

func requireInteractiveProfileTerminal(terminal Terminal) error {
	if terminal == nil || !terminal.PromptsEnabled() {
		return errProfileSelectionRequiresTerminal
	}
	return nil
}

func interactiveProfileError(err error, usage, command string) error {
	switch {
	case errors.Is(err, errProfileSelectionRequiresTerminal):
		return managementUsageError(err.Error(), usage, command)
	case errors.Is(err, errNoProfilesConfigured):
		return managementUsageError(
			"no profiles configured; add one with 'vlt profile add NAME --address URL --username USER'",
			usage,
			command,
		)
	default:
		return safeManagementError(err)
	}
}
