package cli

import "fmt"

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

_vlt_completion() {
    local current command subcommand profiles
    COMPREPLY=()
    current="${COMP_WORDS[COMP_CWORD]}"
    command="${COMP_WORDS[1]-}"
    subcommand="${COMP_WORDS[2]-}"

    case "$command" in
        "")
            COMPREPLY=( $(compgen -W "profile switch completion --profile -h --help" -- "$current") )
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
            case "$subcommand" in
                "")
                    COMPREPLY=( $(compgen -W "add list show update remove -h --help" -- "$current") )
                    ;;
                add)
                    if (( COMP_CWORD >= 3 )); then
                        COMPREPLY=( $(compgen -W "--address --username --auth-path --namespace -h --help" -- "$current") )
                    fi
                    ;;
                list)
                    COMPREPLY=( $(compgen -W "-h --help" -- "$current") )
                    ;;
                show|remove)
                    if (( COMP_CWORD == 3 )); then
                        profiles="$(_vlt_completion_profiles)"
                        COMPREPLY=( $(compgen -W "$profiles -h --help" -- "$current") )
                    fi
                    ;;
                update)
                    if (( COMP_CWORD == 3 )); then
                        profiles="$(_vlt_completion_profiles)"
                        COMPREPLY=( $(compgen -W "$profiles -h --help" -- "$current") )
                    elif (( COMP_CWORD > 3 )); then
                        COMPREPLY=( $(compgen -W "--address --username --auth-path --namespace -h --help" -- "$current") )
                    fi
                    ;;
            esac
            return 0
            ;;
        --profile)
            if (( COMP_CWORD == 2 )); then
                profiles="$(_vlt_completion_profiles)"
                COMPREPLY=( $(compgen -W "$profiles" -- "$current") )
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

_vlt() {
    local command="${words[2]-}"
    local subcommand="${words[3]-}"

    case "$command" in
        "")
            _values 'vlt command' \
                'profile:manage profiles' \
                'switch:select the active profile' \
                'completion:generate shell completion' \
                '--profile:use one profile for a delegated command' \
                '-h:show help' '--help:show help'
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
            case "$subcommand" in
                "")
                    _values 'profile command' 'add' 'list' 'show' 'update' 'remove' '-h' '--help'
                    ;;
                add)
                    if (( CURRENT >= 4 )); then
                        _values 'option' '--address' '--username' '--auth-path' '--namespace' '-h' '--help'
                    fi
                    ;;
                list)
                    _values 'option' '-h' '--help'
                    ;;
                show|remove)
                    if (( CURRENT == 4 )); then
                        _vlt_completion_profiles
                    fi
                    ;;
                update)
                    if (( CURRENT == 4 )); then
                        _vlt_completion_profiles
                    elif (( CURRENT > 4 )); then
                        _values 'option' '--address' '--username' '--auth-path' '--namespace' '-h' '--help'
                    fi
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

function __vlt_token_count_is
    set -l tokens (commandline -opc)
    test (count $tokens) -eq $argv[1]
end

function __vlt_token_count_at_least
    set -l tokens (commandline -opc)
    test (count $tokens) -ge $argv[1]
end

complete -c vlt -f
complete -c vlt -n '__vlt_needs_command' -a 'profile switch completion'
complete -c vlt -n '__vlt_needs_command' -s h -l help
complete -c vlt -n '__vlt_needs_command' -l profile -r -a '(vlt completion __profiles 2>/dev/null)'

complete -c vlt -n '__vlt_using_command completion; and __vlt_token_count_is 2' -a 'bash zsh fish'
complete -c vlt -n '__vlt_using_command completion' -s h -l help

complete -c vlt -n '__vlt_using_command switch; and __vlt_token_count_is 2' -a '(vlt completion __profiles 2>/dev/null)'
complete -c vlt -n '__vlt_using_command switch' -s h -l help

complete -c vlt -n '__vlt_using_command profile; and __vlt_token_count_is 2' -a 'add list show update remove'
complete -c vlt -n '__vlt_using_command profile' -s h -l help

complete -c vlt -n '__vlt_using_profile_subcommand add; and __vlt_token_count_at_least 3' -l address -r
complete -c vlt -n '__vlt_using_profile_subcommand add; and __vlt_token_count_at_least 3' -l username -r
complete -c vlt -n '__vlt_using_profile_subcommand add; and __vlt_token_count_at_least 3' -l auth-path -r
complete -c vlt -n '__vlt_using_profile_subcommand add; and __vlt_token_count_at_least 3' -l namespace -r
complete -c vlt -n '__vlt_using_profile_subcommand add' -s h -l help

complete -c vlt -n '__vlt_using_profile_subcommand list' -s h -l help
complete -c vlt -n '__vlt_using_profile_subcommand show; and __vlt_token_count_is 3' -a '(vlt completion __profiles 2>/dev/null)'
complete -c vlt -n '__vlt_using_profile_subcommand show' -s h -l help
complete -c vlt -n '__vlt_using_profile_subcommand update; and __vlt_token_count_is 3' -a '(vlt completion __profiles 2>/dev/null)'
complete -c vlt -n '__vlt_using_profile_subcommand update; and __vlt_token_count_at_least 4' -l address -r
complete -c vlt -n '__vlt_using_profile_subcommand update; and __vlt_token_count_at_least 4' -l username -r
complete -c vlt -n '__vlt_using_profile_subcommand update; and __vlt_token_count_at_least 4' -l auth-path -r
complete -c vlt -n '__vlt_using_profile_subcommand update; and __vlt_token_count_at_least 4' -l namespace -r
complete -c vlt -n '__vlt_using_profile_subcommand update' -s h -l help
complete -c vlt -n '__vlt_using_profile_subcommand remove; and __vlt_token_count_is 3' -a '(vlt completion __profiles 2>/dev/null)'
complete -c vlt -n '__vlt_using_profile_subcommand remove' -s h -l help
`
