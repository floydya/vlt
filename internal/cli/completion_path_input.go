package cli

import (
	"strings"
	"unicode"

	"vlt/internal/profile"
)

type pathCompletionInput struct {
	profile string
	command string
	mount   string
	prefix  string
}

func pathCompletionRequest(line string) (pathCompletionInput, bool) {
	words, ok := completionWords(line)
	if !ok || len(words) < 3 || words[0] != "vlt" {
		return pathCompletionInput{}, false
	}
	request := pathCompletionInput{}
	index := 1
	if words[index] == "--profile" {
		if len(words) < 5 || profile.ValidateName(words[index+1]) != nil {
			return pathCompletionInput{}, false
		}
		request.profile = words[index+1]
		index += 2
	}
	if index >= len(words)-1 {
		return pathCompletionInput{}, false
	}
	switch words[index] {
	case "read":
		request.command = "read"
		index++
	case "kv":
		if index+2 >= len(words) || words[index+1] != "get" {
			return pathCompletionInput{}, false
		}
		request.command = "kv get"
		index += 2
	default:
		return pathCompletionInput{}, false
	}
	for index < len(words)-1 {
		word := words[index]
		if word == "--" {
			index++
			continue
		}
		name, value, attached := strings.Cut(word, "=")
		if name == "-mount" && request.command == "kv get" {
			if !attached {
				index++
				if index >= len(words)-1 {
					return pathCompletionInput{}, false
				}
				value = words[index]
			}
			if request.mount != "" || value == "" {
				return pathCompletionInput{}, false
			}
			request.mount = value
			index++
			continue
		}
		switch name {
		case "-format", "-field", "-wrap-ttl", "-namespace", "-address":
			if !attached {
				index++
				if index >= len(words)-1 {
					return pathCompletionInput{}, false
				}
			}
		case "-version":
			if request.command != "kv get" {
				return pathCompletionInput{}, false
			}
			if !attached {
				index++
				if index >= len(words)-1 {
					return pathCompletionInput{}, false
				}
			}
		default:
			return pathCompletionInput{}, false
		}
		index++
	}
	request.prefix = words[len(words)-1]
	if strings.HasPrefix(request.prefix, "-") {
		return pathCompletionInput{}, false
	}
	return request, true
}

func completionWords(line string) ([]string, bool) {
	if len(line) > 4096 || strings.IndexFunc(line, func(character rune) bool {
		return unicode.IsControl(character) && character != '\t'
	}) >= 0 {
		return nil, false
	}
	var words []string
	var word strings.Builder
	var quote rune
	active, escaped := false, false
	for _, character := range line {
		if escaped {
			word.WriteRune(character)
			escaped = false
			continue
		}
		if character == '\\' && quote != '\'' {
			escaped = true
			active = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
			} else {
				word.WriteRune(character)
			}
			continue
		}
		switch {
		case character == '\'' || character == '"':
			quote = character
			active = true
		case unicode.IsSpace(character):
			if active {
				words = append(words, word.String())
				word.Reset()
				active = false
			}
		case strings.ContainsRune(";|&<>`", character):
			return nil, false
		default:
			word.WriteRune(character)
			active = true
		}
	}
	if escaped {
		return nil, false
	}
	words = append(words, word.String())
	return words, true
}
