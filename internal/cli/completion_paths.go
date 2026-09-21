package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
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

func completeKVv1Paths(ctx context.Context, selected pathCompletionContext, prefix string, transport http.RoundTripper) []string {
	if transport == nil || selected.token == "" || profile.ValidateAddress(selected.profile.Address) != nil {
		return nil
	}
	address, err := url.Parse(selected.profile.Address)
	if err != nil || strings.EqualFold(address.Scheme, "http") && !selected.profile.AllowInsecure {
		return nil
	}
	separator := strings.LastIndexByte(prefix, '/')
	if separator < 0 {
		return nil
	}
	directory, partial := prefix[:separator+1], prefix[separator+1:]
	if !validCompletionPath(directory) || strings.IndexFunc(partial, unicode.IsControl) >= 0 || strings.ContainsAny(partial, `/\`) {
		return nil
	}
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	listed, ok := requestVaultCompletionJSON(ctx, client, selected, "LIST", directory, nil)
	if !ok {
		return nil
	}
	var listing struct {
		Data struct {
			Keys []string `json:"keys"`
		} `json:"data"`
	}
	if json.Unmarshal(listed, &listing) != nil {
		return nil
	}
	seen := make(map[string]bool)
	var paths []string
	for _, key := range listing.Data.Keys {
		if !validCompletionLeaf(key) || !strings.HasPrefix(key, partial) {
			continue
		}
		path := directory + key
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	sort.Strings(paths)
	requestBody, err := json.Marshal(struct {
		Paths []string `json:"paths"`
	}{Paths: paths})
	if err != nil {
		return nil
	}
	checked, ok := requestVaultCompletionJSON(ctx, client, selected, http.MethodPost, "sys/capabilities-self", requestBody)
	if !ok {
		return nil
	}
	var capabilityMap map[string]json.RawMessage
	if json.Unmarshal(checked, &capabilityMap) != nil {
		return nil
	}
	var readable []string
	for _, path := range paths {
		entry := capabilityMap[path]
		if len(entry) == 0 && len(paths) == 1 {
			entry = capabilityMap["capabilities"]
		}
		var capabilities []string
		if json.Unmarshal(entry, &capabilities) != nil {
			continue
		}
		canRead, denied := false, false
		for _, capability := range capabilities {
			switch capability {
			case "read":
				canRead = true
			case "deny":
				denied = true
			}
		}
		if canRead && !denied {
			readable = append(readable, path)
		}
	}
	return readable
}

func requestVaultCompletionJSON(ctx context.Context, client *http.Client, selected pathCompletionContext, method, path string, body []byte) ([]byte, bool) {
	address, err := url.Parse(selected.profile.Address)
	if err != nil {
		return nil, false
	}
	address.Path = strings.TrimRight(address.Path, "/") + "/v1/" + path
	address.RawPath = ""
	request, err := http.NewRequestWithContext(ctx, method, address.String(), bytes.NewReader(body))
	if err != nil {
		return nil, false
	}
	request.Header.Set("X-Vault-Token", selected.token)
	if selected.profile.Namespace != "" {
		request.Header.Set("X-Vault-Namespace", selected.profile.Namespace)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, false
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, false
	}
	const maxBody = 1 << 20
	result, err := io.ReadAll(io.LimitReader(response.Body, maxBody+1))
	if err != nil || len(result) > maxBody {
		return nil, false
	}
	return result, true
}

func validCompletionPath(path string) bool {
	if !strings.HasSuffix(path, "/") {
		return false
	}
	for _, part := range strings.Split(strings.TrimSuffix(path, "/"), "/") {
		if !validCompletionLeaf(part) {
			return false
		}
	}
	return true
}

func validCompletionLeaf(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, `/\`) && strings.IndexFunc(value, unicode.IsControl) < 0
}
