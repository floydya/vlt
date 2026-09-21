package cli

import (
	"context"
	"strings"
	"unicode"

	"vlt/internal/profile"
)

type pathCompletionContext struct {
	profile profile.Profile
	token   string
}

func resolvePathCompletionContext(ctx context.Context, explicitName string, dependencies CompletionDependencies) (pathCompletionContext, bool) {
	if ctx.Err() != nil || dependencies.Profiles == nil || dependencies.Credentials == nil {
		return pathCompletionContext{}, false
	}
	if explicitName != "" && profile.ValidateName(explicitName) != nil {
		return pathCompletionContext{}, false
	}
	configuration, err := dependencies.Profiles.Load(ctx)
	if err != nil {
		return pathCompletionContext{}, false
	}
	name := explicitName
	if name == "" {
		name = configuration.ActiveProfile
	}
	if profile.ValidateName(name) != nil {
		return pathCompletionContext{}, false
	}
	selected, err := profile.NewService(configuration.Profiles).Find(name)
	if err != nil || selected.Validate() != nil {
		return pathCompletionContext{}, false
	}
	token, err := dependencies.Credentials.Get(ctx, name)
	if err != nil || strings.TrimSpace(token) == "" || strings.IndexFunc(token, unicode.IsControl) >= 0 {
		return pathCompletionContext{}, false
	}
	return pathCompletionContext{profile: selected, token: token}, true
}
