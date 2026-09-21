package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"vlt/internal/favorite"
	"vlt/internal/profile"
	"vlt/internal/vaultexec"
)

const completionHelpText = `Generate shell completion for vlt commands.

Usage:
  vlt completion SHELL

Shells:
  bash
  zsh
  fish

Options:
  -h, --help  Show help

Examples:
  vlt completion bash
  vlt completion zsh
  vlt completion fish
`

type CompletionDependencies struct {
	Profiles  ConfigurationLoader
	Favorites FavoriteConfigurationLoader
	Vault     interface {
		Execute(context.Context, vaultexec.Invocation) (vaultexec.Result, error)
	}
	Output io.Writer
}

func NewCompletionHandler(dependencies CompletionDependencies) Handler {
	return func(ctx context.Context, args []string) error {
		if len(args) == 2 && args[0] == "__vault_commands" {
			return writeCompletionVaultCommands(ctx, args[1], dependencies)
		}
		if containsHelpFlag(args) {
			return writeManagementHelp(dependencies.Output, "completion", completionHelpText)
		}
		if len(args) == 1 && args[0] == "__profiles" {
			return writeCompletionProfiles(ctx, dependencies)
		}
		if len(args) == 1 && args[0] == "__favorites" {
			return writeCompletionFavorites(ctx, dependencies)
		}
		if len(args) == 0 {
			return AutomaticHelp{Text: completionHelpText}
		}
		if len(args) > 1 {
			return managementUsageError(fmt.Sprintf("unexpected argument %q", args[1]), "vlt completion SHELL", "vlt completion")
		}
		script, err := completionScript(args[0])
		if err != nil {
			return err
		}
		if dependencies.Output == nil {
			return fmt.Errorf("display completion script: output is not configured")
		}
		if _, err := io.WriteString(dependencies.Output, script); err != nil {
			return fmt.Errorf("display completion script: %w", err)
		}
		return nil
	}
}

func writeCompletionVaultCommands(ctx context.Context, prefix string, dependencies CompletionDependencies) error {
	if dependencies.Vault == nil || (prefix != "" && !validVaultCommand(prefix)) {
		return nil
	}
	line := "vault " + prefix
	result, err := dependencies.Vault.Execute(ctx, vaultexec.Invocation{
		Environment: vaultexec.EnvironmentOverlay{
			Set: map[string]string{
				"COMP_LINE":  line,
				"COMP_POINT": strconv.Itoa(len(line)),
				"VAULT_ADDR": "not-a-url",
			},
			Unset: []string{"VAULT_TOKEN", "VAULT_NAMESPACE"},
		},
		Mode: vaultexec.Captured,
	})
	if err != nil {
		return nil
	}
	reserved := map[string]bool{"profile": true, "switch": true, "favorite": true, "completion": true}
	candidates := make(map[string]bool)
	for _, candidate := range strings.Split(string(result.Stdout), "\n") {
		if validVaultCommand(candidate) && strings.HasPrefix(candidate, prefix) && !reserved[candidate] {
			candidates[candidate] = true
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	ordered := make([]string, 0, len(candidates))
	for candidate := range candidates {
		ordered = append(ordered, candidate)
	}
	sort.Strings(ordered)
	if dependencies.Output == nil {
		return fmt.Errorf("display completion candidates: output is not configured")
	}
	_, err = io.WriteString(dependencies.Output, strings.Join(ordered, "\n")+"\n")
	if err != nil {
		return fmt.Errorf("display completion candidates: %w", err)
	}
	return nil
}

func validVaultCommand(value string) bool {
	if value == "" || value[0] < 'a' || value[0] > 'z' {
		return false
	}
	for _, character := range value[1:] {
		if character >= 'a' && character <= 'z' || character >= '0' && character <= '9' || character == '-' || character == '_' {
			continue
		}
		return false
	}
	return true
}

func writeCompletionFavorites(ctx context.Context, dependencies CompletionDependencies) error {
	if dependencies.Favorites == nil {
		return nil
	}
	configuration, err := dependencies.Favorites.Load(ctx)
	if err != nil {
		return nil
	}
	var output strings.Builder
	for _, candidate := range favorite.NewService(configuration.Favorites).List() {
		if candidate.ID != "" {
			fmt.Fprintln(&output, candidate.ID)
		}
	}
	if output.Len() == 0 {
		return nil
	}
	if dependencies.Output == nil {
		return fmt.Errorf("display completion candidates: output is not configured")
	}
	if _, err := io.WriteString(dependencies.Output, output.String()); err != nil {
		return fmt.Errorf("display completion candidates: %w", err)
	}
	return nil
}

func writeCompletionProfiles(ctx context.Context, dependencies CompletionDependencies) error {
	if dependencies.Profiles == nil {
		return nil
	}
	configuration, err := dependencies.Profiles.Load(ctx)
	if err != nil {
		return nil
	}
	profiles := profile.NewService(configuration.Profiles).List()
	if len(profiles) == 0 {
		return nil
	}
	var output strings.Builder
	for _, candidate := range profiles {
		fmt.Fprintln(&output, candidate.Name)
	}
	if dependencies.Output == nil {
		return fmt.Errorf("display completion candidates: output is not configured")
	}
	if _, err := io.WriteString(dependencies.Output, output.String()); err != nil {
		return fmt.Errorf("display completion candidates: %w", err)
	}
	return nil
}

func completionScript(shell string) (string, error) {
	switch shell {
	case "bash":
		return bashCompletionScript, nil
	case "zsh":
		return zshCompletionScript, nil
	case "fish":
		return fishCompletionScript, nil
	default:
		return "", managementUsageError(
			fmt.Sprintf("unsupported shell %q; choose bash, zsh, or fish", shell),
			"vlt completion SHELL",
			"vlt completion",
		)
	}
}

const bashCompletionScript = `_vlt_completion_profiles() {
    vlt completion __profiles 2>/dev/null
}

_vlt_completion_favorites() {
    vlt completion __favorites 2>/dev/null
}

_vlt_completion_root_commands() {
    local current="$1" candidates candidate existing duplicate
    COMPREPLY=( $(compgen -W "profile switch favorite completion --profile -h --help" -- "$current") )
    if [[ "$current" == -* ]]; then
        return 0
    fi
    candidates="$(vlt completion __vault_commands "$current" 2>/dev/null)" || return 0
    while IFS= read -r candidate; do
        case "$candidate" in
            ""|profile|switch|favorite|completion) continue ;;
        esac
        if [[ "$candidate" != "$current"* ]]; then
            continue
        fi
        duplicate=0
        for existing in "${COMPREPLY[@]}"; do
            if [[ "$existing" == "$candidate" ]]; then
                duplicate=1
                break
            fi
        done
        if (( duplicate == 0 )); then
            COMPREPLY+=("$candidate")
        fi
    done <<< "$candidates"
}

_vlt_completion() {
    local current previous command subcommand profiles favorites
    COMPREPLY=()
    current="${COMP_WORDS[COMP_CWORD]}"
    previous=""
    if (( COMP_CWORD > 0 )); then
        previous="${COMP_WORDS[COMP_CWORD-1]}"
    fi
    command="${COMP_WORDS[1]-}"
    subcommand="${COMP_WORDS[2]-}"

    if (( COMP_CWORD == 1 )); then
        _vlt_completion_root_commands "$current"
        return 0
    fi

    case "$command" in
        "")
            _vlt_completion_root_commands "$current"
            ;;
        completion)
            if (( COMP_CWORD == 2 )); then
                COMPREPLY=( $(compgen -W "bash zsh fish -h --help" -- "$current") )
            fi
            return 0
            ;;
        switch)
            if (( COMP_CWORD == 2 )); then
                profiles="$(_vlt_completion_profiles)"
                COMPREPLY=( $(compgen -W "$profiles -h --help" -- "$current") )
            fi
            return 0
            ;;
        profile)
            if (( COMP_CWORD == 2 )); then
                COMPREPLY=( $(compgen -W "add list current show update remove -h --help" -- "$current") )
                return 0
            fi
            case "$subcommand" in
                "")
                    COMPREPLY=( $(compgen -W "add list current show update remove -h --help" -- "$current") )
                    ;;
                add)
                    if (( COMP_CWORD >= 3 )); then
                        COMPREPLY=( $(compgen -W "--address --username --auth-path --namespace --allow-insecure --color -h --help" -- "$current") )
                    fi
                    ;;
                list|current)
                    COMPREPLY=( $(compgen -W "-h --help" -- "$current") )
                    ;;
                show)
                    if (( COMP_CWORD == 3 )); then
                        profiles="$(_vlt_completion_profiles)"
                        COMPREPLY=( $(compgen -W "$profiles -h --help" -- "$current") )
                    fi
                    ;;
                remove)
                    if (( COMP_CWORD == 3 )); then
                        profiles="$(_vlt_completion_profiles)"
                        COMPREPLY=( $(compgen -W "$profiles -h --help" -- "$current") )
                    elif (( COMP_CWORD > 3 )); then
                        COMPREPLY=( $(compgen -W "--remove-favorites -h --help" -- "$current") )
                    fi
                    ;;
                update)
                    if (( COMP_CWORD == 3 )); then
                        profiles="$(_vlt_completion_profiles)"
                        COMPREPLY=( $(compgen -W "$profiles -h --help" -- "$current") )
                    elif (( COMP_CWORD > 3 )); then
                        COMPREPLY=( $(compgen -W "--address --username --auth-path --namespace --allow-insecure --color -h --help" -- "$current") )
                    fi
                    ;;
            esac
            return 0
            ;;
        favorite)
            if (( COMP_CWORD == 2 )); then
                COMPREPLY=( $(compgen -W "add list update remove -h --help" -- "$current") )
                return 0
            fi
            case "$subcommand" in
                "")
                    COMPREPLY=( $(compgen -W "add list update remove -h --help" -- "$current") )
                    ;;
                add)
                    case "$previous" in
                        --profile)
                            profiles="$(_vlt_completion_profiles)"
                            COMPREPLY=( $(compgen -W "$profiles" -- "$current") )
                            ;;
                        --operation)
                            COMPREPLY=( $(compgen -W "read kv-get" -- "$current") )
                            ;;
                        --note)
                            ;;
                        *)
                            if (( COMP_CWORD >= 3 )); then
                                COMPREPLY=( $(compgen -W "--profile --operation --note -h --help" -- "$current") )
                            fi
                            ;;
                    esac
                    ;;
                update)
                    if (( COMP_CWORD == 3 )); then
                        favorites="$(_vlt_completion_favorites)"
                        COMPREPLY=( $(compgen -W "$favorites -h --help" -- "$current") )
                        return 0
                    fi
                    case "$previous" in
                        --profile)
                            profiles="$(_vlt_completion_profiles)"
                            COMPREPLY=( $(compgen -W "$profiles" -- "$current") )
                            ;;
                        --operation)
                            COMPREPLY=( $(compgen -W "read kv-get" -- "$current") )
                            ;;
                        --path|--note)
                            ;;
                        *)
                            if (( COMP_CWORD >= 3 )); then
                                COMPREPLY=( $(compgen -W "--profile --operation --path --note -h --help" -- "$current") )
                            fi
                            ;;
                    esac
                    ;;
                remove)
                    if (( COMP_CWORD == 3 )); then
                        favorites="$(_vlt_completion_favorites)"
                        COMPREPLY=( $(compgen -W "$favorites -h --help" -- "$current") )
                    fi
                    ;;
                list)
                    COMPREPLY=( $(compgen -W "-h --help" -- "$current") )
                    ;;
            esac
            return 0
            ;;
        --profile)
            if (( COMP_CWORD == 2 )); then
                profiles="$(_vlt_completion_profiles)"
                COMPREPLY=( $(compgen -W "$profiles" -- "$current") )
            elif (( COMP_CWORD == 3 )); then
                _vlt_completion_root_commands "$current"
            fi
            return 0
            ;;
        -h|--help)
            return 0
            ;;
        *)
            return 0
            ;;
    esac
}

complete -F _vlt_completion vlt
`

const zshCompletionScript = `#compdef vlt

_vlt_completion_profiles() {
    local -a profiles
    profiles=("${(@f)$(vlt completion __profiles 2>/dev/null)}")
    _describe 'profile' profiles
}

_vlt_completion_favorites() {
    local -a favorites
    favorites=("${(@f)$(vlt completion __favorites 2>/dev/null)}")
    _describe 'favorite ID' favorites
}

_vlt() {
    local command="${words[2]-}"
    local subcommand="${words[3]-}"
    local previous="${words[CURRENT-1]-}"
    local -a commands
    commands=(
        'profile:manage profiles'
        'switch:select the active profile'
        'favorite:manage favorites'
        'completion:generate shell completion'
        '--profile:use one profile for a delegated command'
        '-h:show help' '--help:show help'
    )

    if (( CURRENT == 2 )); then
        _describe 'vlt command' commands
        return 0
    fi

    case "$command" in
        "")
            _describe 'vlt command' commands
            ;;
        completion)
            if (( CURRENT == 3 )); then
                _values 'shell' 'bash' 'zsh' 'fish' '-h' '--help'
            fi
            return 0
            ;;
        switch)
            if (( CURRENT == 3 )); then
                _vlt_completion_profiles
            fi
            return 0
            ;;
        profile)
            if (( CURRENT == 3 )); then
                _values 'profile command' 'add' 'list' 'current' 'show' 'update' 'remove' '-h' '--help'
                return 0
            fi
            case "$subcommand" in
                "")
                    _values 'profile command' 'add' 'list' 'current' 'show' 'update' 'remove' '-h' '--help'
                    ;;
                add)
                    if (( CURRENT >= 4 )); then
                        _values 'option' '--address' '--username' '--auth-path' '--namespace' '--allow-insecure' '--color' '-h' '--help'
                    fi
                    ;;
                list|current)
                    _values 'option' '-h' '--help'
                    ;;
                show)
                    if (( CURRENT == 4 )); then
                        _vlt_completion_profiles
                    fi
                    ;;
                remove)
                    if (( CURRENT == 4 )); then
                        _vlt_completion_profiles
                    elif (( CURRENT > 4 )); then
                        _values 'option' '--remove-favorites' '-h' '--help'
                    fi
                    ;;
                update)
                    if (( CURRENT == 4 )); then
                        _vlt_completion_profiles
                    elif (( CURRENT > 4 )); then
                        _values 'option' '--address' '--username' '--auth-path' '--namespace' '--allow-insecure' '--color' '-h' '--help'
                    fi
                    ;;
            esac
            return 0
            ;;
        favorite)
            if (( CURRENT == 3 )); then
                _values 'favorite command' 'add' 'list' 'update' 'remove' '-h' '--help'
                return 0
            fi
            case "$subcommand" in
                "")
                    _values 'favorite command' 'add' 'list' 'update' 'remove' '-h' '--help'
                    ;;
                add)
                    case "$previous" in
                        --profile)
                            _vlt_completion_profiles
                            ;;
                        --operation)
                            _values 'operation' 'read' 'kv-get'
                            ;;
                        --note)
                            ;;
                        *)
                            _values 'option' '--profile' '--operation' '--note' '-h' '--help'
                            ;;
                    esac
                    ;;
                update)
                    if (( CURRENT == 4 )); then
                        _vlt_completion_favorites
                        return 0
                    fi
                    case "$previous" in
                        --profile)
                            _vlt_completion_profiles
                            ;;
                        --operation)
                            _values 'operation' 'read' 'kv-get'
                            ;;
                        --path|--note)
                            ;;
                        *)
                            _values 'option' '--profile' '--operation' '--path' '--note' '-h' '--help'
                            ;;
                    esac
                    ;;
                remove)
                    if (( CURRENT == 4 )); then
                        _vlt_completion_favorites
                    fi
                    ;;
                list)
                    _values 'option' '-h' '--help'
                    ;;
            esac
            return 0
            ;;
        --profile)
            if (( CURRENT == 3 )); then
                _vlt_completion_profiles
            fi
            return 0
            ;;
        -h|--help)
            return 0
            ;;
        *)
            return 0
            ;;
    esac
}

compdef _vlt vlt
`

const fishCompletionScript = `function __vlt_needs_command
    set -l tokens (commandline -opc)
    test (count $tokens) -eq 1
end

function __vlt_using_command
    set -l tokens (commandline -opc)
    test (count $tokens) -ge 2; and test "$tokens[2]" = "$argv[1]"
end

function __vlt_using_profile_subcommand
    set -l tokens (commandline -opc)
    test (count $tokens) -ge 3; and test "$tokens[2]" = profile; and test "$tokens[3]" = "$argv[1]"
end

function __vlt_using_favorite_subcommand
    set -l tokens (commandline -opc)
    test (count $tokens) -ge 3; and test "$tokens[2]" = favorite; and test "$tokens[3]" = "$argv[1]"
end

function __vlt_token_count_is
    set -l tokens (commandline -opc)
    test (count $tokens) -eq $argv[1]
end

function __vlt_token_count_at_least
    set -l tokens (commandline -opc)
    test (count $tokens) -ge $argv[1]
end

complete -c vlt -f
complete -c vlt -n '__vlt_needs_command' -a 'profile switch favorite completion'
complete -c vlt -n '__vlt_needs_command' -s h -l help
complete -c vlt -n '__vlt_needs_command' -l profile -r -a '(vlt completion __profiles 2>/dev/null)'

complete -c vlt -n '__vlt_using_command completion; and __vlt_token_count_is 2' -a 'bash zsh fish'
complete -c vlt -n '__vlt_using_command completion' -s h -l help

complete -c vlt -n '__vlt_using_command switch; and __vlt_token_count_is 2' -a '(vlt completion __profiles 2>/dev/null)'
complete -c vlt -n '__vlt_using_command switch' -s h -l help

complete -c vlt -n '__vlt_using_command profile; and __vlt_token_count_is 2' -a 'add list current show update remove'
complete -c vlt -n '__vlt_using_command profile' -s h -l help

complete -c vlt -n '__vlt_using_profile_subcommand add; and __vlt_token_count_at_least 3' -l address -r
complete -c vlt -n '__vlt_using_profile_subcommand add; and __vlt_token_count_at_least 3' -l username -r
complete -c vlt -n '__vlt_using_profile_subcommand add; and __vlt_token_count_at_least 3' -l auth-path -r
complete -c vlt -n '__vlt_using_profile_subcommand add; and __vlt_token_count_at_least 3' -l namespace -r
complete -c vlt -n '__vlt_using_profile_subcommand add; and __vlt_token_count_at_least 3' -l allow-insecure
complete -c vlt -n '__vlt_using_profile_subcommand add; and __vlt_token_count_at_least 3' -l color -r
complete -c vlt -n '__vlt_using_profile_subcommand add' -s h -l help

complete -c vlt -n '__vlt_using_profile_subcommand list' -s h -l help
complete -c vlt -n '__vlt_using_profile_subcommand current' -s h -l help
complete -c vlt -n '__vlt_using_profile_subcommand show; and __vlt_token_count_is 3' -a '(vlt completion __profiles 2>/dev/null)'
complete -c vlt -n '__vlt_using_profile_subcommand show' -s h -l help
complete -c vlt -n '__vlt_using_profile_subcommand update; and __vlt_token_count_is 3' -a '(vlt completion __profiles 2>/dev/null)'
complete -c vlt -n '__vlt_using_profile_subcommand update; and __vlt_token_count_at_least 4' -l address -r
complete -c vlt -n '__vlt_using_profile_subcommand update; and __vlt_token_count_at_least 4' -l username -r
complete -c vlt -n '__vlt_using_profile_subcommand update; and __vlt_token_count_at_least 4' -l auth-path -r
complete -c vlt -n '__vlt_using_profile_subcommand update; and __vlt_token_count_at_least 4' -l namespace -r
complete -c vlt -n '__vlt_using_profile_subcommand update; and __vlt_token_count_at_least 4' -l allow-insecure
complete -c vlt -n '__vlt_using_profile_subcommand update; and __vlt_token_count_at_least 4' -l color -r
complete -c vlt -n '__vlt_using_profile_subcommand update' -s h -l help
complete -c vlt -n '__vlt_using_profile_subcommand remove; and __vlt_token_count_is 3' -a '(vlt completion __profiles 2>/dev/null)'
complete -c vlt -n '__vlt_using_profile_subcommand remove; and __vlt_token_count_at_least 4' -l remove-favorites
complete -c vlt -n '__vlt_using_profile_subcommand remove' -s h -l help

complete -c vlt -n '__vlt_using_command favorite; and __vlt_token_count_is 2' -a 'add list update remove'
complete -c vlt -n '__vlt_using_command favorite' -s h -l help

complete -c vlt -n '__vlt_using_favorite_subcommand add; and __vlt_token_count_at_least 3' -l profile -r -a '(vlt completion __profiles 2>/dev/null)'
complete -c vlt -n '__vlt_using_favorite_subcommand add; and __vlt_token_count_at_least 3' -l operation -r -a 'read kv-get'
complete -c vlt -n '__vlt_using_favorite_subcommand add; and __vlt_token_count_at_least 3' -l note -r
complete -c vlt -n '__vlt_using_favorite_subcommand add' -s h -l help

complete -c vlt -n '__vlt_using_favorite_subcommand list' -s h -l help

complete -c vlt -n '__vlt_using_favorite_subcommand update; and __vlt_token_count_is 3' -a '(vlt completion __favorites 2>/dev/null)'
complete -c vlt -n '__vlt_using_favorite_subcommand update; and __vlt_token_count_at_least 3' -l profile -r -a '(vlt completion __profiles 2>/dev/null)'
complete -c vlt -n '__vlt_using_favorite_subcommand update; and __vlt_token_count_at_least 3' -l operation -r -a 'read kv-get'
complete -c vlt -n '__vlt_using_favorite_subcommand update; and __vlt_token_count_at_least 3' -l path -r
complete -c vlt -n '__vlt_using_favorite_subcommand update; and __vlt_token_count_at_least 3' -l note -r
complete -c vlt -n '__vlt_using_favorite_subcommand update' -s h -l help

complete -c vlt -n '__vlt_using_favorite_subcommand remove; and __vlt_token_count_is 3' -a '(vlt completion __favorites 2>/dev/null)'
complete -c vlt -n '__vlt_using_favorite_subcommand remove' -s h -l help
`
