package profile

import (
	"context"
	"errors"
	"fmt"
	"reflect"
)

var ErrCredentialNotFound = errors.New("credential not found")

type Configuration struct {
	Profiles      []Profile
	ActiveProfile string
}

type ConfigurationStore interface {
	Load(context.Context) (Configuration, error)
	Save(context.Context, Configuration) error
}

type CredentialStore interface {
	Get(context.Context, string) (string, error)
	Set(context.Context, string, string) error
	Delete(context.Context, string) error
}

type ProfileAuthenticator interface {
	Login(context.Context, Profile) error
}

type MutationService struct {
	configurations ConfigurationStore
	credentials    CredentialStore
	authenticator  ProfileAuthenticator
}

type ProfileChanges struct {
	Address       *string
	Username      *string
	AuthPath      *string
	Namespace     *string
	AllowInsecure *bool
	Color         *string
}

type credentialSnapshot struct {
	token  string
	exists bool
}

func NewMutationService(configurations ConfigurationStore, credentials CredentialStore, authenticator ProfileAuthenticator) *MutationService {
	return &MutationService{
		configurations: configurations,
		credentials:    credentials,
		authenticator:  authenticator,
	}
}

func (s *MutationService) Add(ctx context.Context, candidate Profile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := candidate.Validate(); err != nil {
		return fmt.Errorf("add profile: invalid profile: %w", err)
	}
	if err := s.validateStorageDependencies(); err != nil {
		return fmt.Errorf("add profile: %w", err)
	}
	if s.authenticator == nil {
		return errors.New("add profile: profile authenticator is not configured")
	}

	original, err := s.configurations.Load(ctx)
	if err != nil {
		return fmt.Errorf("add profile: load profiles: %w", err)
	}
	for _, existing := range original.Profiles {
		if existing.Name == candidate.Name {
			return fmt.Errorf("add profile %q: profile already exists", candidate.Name)
		}
	}

	updated := cloneConfiguration(original)
	updated.Profiles = append(updated.Profiles, candidate)
	if err := s.configurations.Save(ctx, updated); err != nil {
		cleanupErrors := s.removeIncompleteAdd(context.WithoutCancel(ctx), candidate.Name, original)
		return joinMutationErrors(fmt.Errorf("add profile %q: persist profile: %w", candidate.Name, err), cleanupErrors)
	}
	if err := s.authenticator.Login(ctx, candidate); err != nil {
		cleanupErrors := s.removeIncompleteAdd(context.WithoutCancel(ctx), candidate.Name, original)
		return joinMutationErrors(fmt.Errorf("add profile %q: authenticate profile: %w", candidate.Name, err), cleanupErrors)
	}
	return nil
}

func (s *MutationService) Update(ctx context.Context, name string, changes ProfileChanges) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateName(name); err != nil {
		return fmt.Errorf("update profile: invalid profile name: %w", err)
	}
	if err := s.validateStorageDependencies(); err != nil {
		return fmt.Errorf("update profile: %w", err)
	}
	if s.authenticator == nil {
		return errors.New("update profile: profile authenticator is not configured")
	}

	original, err := s.configurations.Load(ctx)
	if err != nil {
		return fmt.Errorf("update profile %q: load profiles: %w", name, err)
	}
	index := -1
	for candidateIndex, candidate := range original.Profiles {
		if candidate.Name == name {
			index = candidateIndex
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("update profile %q: profile not found", name)
	}

	updatedProfile := applyProfileChanges(original.Profiles[index], changes)
	if err := updatedProfile.Validate(); err != nil {
		return fmt.Errorf("update profile %q: invalid profile: %w", name, err)
	}
	if updatedProfile == original.Profiles[index] {
		return nil
	}
	authProfile := updatedProfile
	authProfile.Color = original.Profiles[index].Color
	if authProfile == original.Profiles[index] {
		updated := cloneConfiguration(original)
		updated.Profiles[index] = updatedProfile
		if err := s.configurations.Save(ctx, updated); err != nil {
			restoreErr := restoreConfiguration(context.WithoutCancel(ctx), s.configurations, original)
			if restoreErr != nil {
				return errors.Join(fmt.Errorf("update profile %q: persist updated profile: %w", name, err), fmt.Errorf("restore profile configuration: %w", restoreErr))
			}
			return fmt.Errorf("update profile %q: persist updated profile: %w", name, err)
		}
		return nil
	}

	originalCredential, err := s.loadCredential(ctx, name)
	if err != nil {
		return fmt.Errorf("update profile %q: load original credential: %w", name, err)
	}
	if err := s.authenticator.Login(ctx, updatedProfile); err != nil {
		cleanupErrors := s.restoreCredential(context.WithoutCancel(ctx), name, originalCredential)
		return joinMutationErrors(fmt.Errorf("update profile %q: authenticate profile: %w", name, err), cleanupErrors)
	}

	updated := cloneConfiguration(original)
	updated.Profiles[index] = updatedProfile
	if err := s.configurations.Save(ctx, updated); err != nil {
		rollbackContext := context.WithoutCancel(ctx)
		var cleanupErrors []error
		if restoreErr := restoreConfiguration(rollbackContext, s.configurations, original); restoreErr != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("restore profile configuration: %w", restoreErr))
		}
		cleanupErrors = append(cleanupErrors, s.restoreCredential(rollbackContext, name, originalCredential)...)
		return joinMutationErrors(fmt.Errorf("update profile %q: persist updated profile: %w", name, err), cleanupErrors)
	}
	return nil
}

func (s *MutationService) Remove(ctx context.Context, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := ValidateName(name); err != nil {
		return fmt.Errorf("remove profile: invalid profile name: %w", err)
	}
	if err := s.validateStorageDependencies(); err != nil {
		return fmt.Errorf("remove profile: %w", err)
	}

	original, err := s.configurations.Load(ctx)
	if err != nil {
		return fmt.Errorf("remove profile %q: load profiles: %w", name, err)
	}
	index := -1
	for candidateIndex, candidate := range original.Profiles {
		if candidate.Name == name {
			index = candidateIndex
			break
		}
	}
	if index < 0 {
		return fmt.Errorf("remove profile %q: profile not found", name)
	}

	originalCredential, err := s.loadCredential(ctx, name)
	if err != nil {
		return fmt.Errorf("remove profile %q: load original credential: %w", name, err)
	}
	updated := cloneConfiguration(original)
	updated.Profiles = append(updated.Profiles[:index], updated.Profiles[index+1:]...)
	if updated.ActiveProfile == name {
		updated.ActiveProfile = ""
	}
	if err := s.configurations.Save(ctx, updated); err != nil {
		cleanupErrors := []error{}
		if restoreErr := restoreConfiguration(context.WithoutCancel(ctx), s.configurations, original); restoreErr != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("restore profile configuration: %w", restoreErr))
		}
		return joinMutationErrors(fmt.Errorf("remove profile %q: persist profile removal: %w", name, err), cleanupErrors)
	}

	if err := s.credentials.Delete(ctx, name); err != nil && !errors.Is(err, ErrCredentialNotFound) {
		rollbackContext := context.WithoutCancel(ctx)
		var cleanupErrors []error
		if restoreErr := restoreConfiguration(rollbackContext, s.configurations, original); restoreErr != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("restore profile configuration: %w", restoreErr))
		}
		cleanupErrors = append(cleanupErrors, s.restoreCredential(rollbackContext, name, originalCredential)...)
		return joinMutationErrors(fmt.Errorf("remove profile %q: delete credential: %w", name, err), cleanupErrors)
	}
	return nil
}

func (s *MutationService) validateStorageDependencies() error {
	if s == nil || s.configurations == nil {
		return errors.New("configuration store is not configured")
	}
	if s.credentials == nil {
		return errors.New("credential store is not configured")
	}
	return nil
}

func (s *MutationService) removeIncompleteAdd(ctx context.Context, name string, original Configuration) []error {
	var cleanupErrors []error
	if err := s.credentials.Delete(ctx, name); err != nil && !errors.Is(err, ErrCredentialNotFound) {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("remove incomplete credential: %w", err))
	}
	if err := restoreConfiguration(ctx, s.configurations, original); err != nil {
		cleanupErrors = append(cleanupErrors, fmt.Errorf("restore profile configuration: %w", err))
	}
	return cleanupErrors
}

func (s *MutationService) loadCredential(ctx context.Context, name string) (credentialSnapshot, error) {
	token, err := s.credentials.Get(ctx, name)
	if errors.Is(err, ErrCredentialNotFound) {
		return credentialSnapshot{}, nil
	}
	if err != nil {
		return credentialSnapshot{}, err
	}
	return credentialSnapshot{token: token, exists: true}, nil
}

func (s *MutationService) restoreCredential(ctx context.Context, name string, snapshot credentialSnapshot) []error {
	if snapshot.exists {
		if err := s.credentials.Set(ctx, name, snapshot.token); err != nil {
			return []error{fmt.Errorf("restore original credential: %w", err)}
		}
		return nil
	}
	if err := s.credentials.Delete(ctx, name); err != nil && !errors.Is(err, ErrCredentialNotFound) {
		return []error{fmt.Errorf("remove replacement credential: %w", err)}
	}
	return nil
}

func applyProfileChanges(original Profile, changes ProfileChanges) Profile {
	updated := original
	if changes.Address != nil {
		updated.Address = *changes.Address
	}
	if changes.Username != nil {
		updated.Username = *changes.Username
	}
	if changes.AuthPath != nil {
		updated.AuthPath = *changes.AuthPath
	}
	if changes.Namespace != nil {
		updated.Namespace = *changes.Namespace
	}
	if changes.AllowInsecure != nil {
		updated.AllowInsecure = *changes.AllowInsecure
	}
	if changes.Color != nil {
		updated.Color = *changes.Color
	}
	return updated
}

func restoreConfiguration(ctx context.Context, store ConfigurationStore, original Configuration) error {
	current, loadErr := store.Load(ctx)
	if loadErr == nil && reflect.DeepEqual(current, original) {
		return nil
	}
	if err := store.Save(ctx, cloneConfiguration(original)); err != nil {
		return err
	}
	return nil
}

func cloneConfiguration(configuration Configuration) Configuration {
	configuration.Profiles = append([]Profile(nil), configuration.Profiles...)
	return configuration
}

func joinMutationErrors(primary error, cleanupErrors []error) error {
	if len(cleanupErrors) == 0 {
		return primary
	}
	return errors.Join(append([]error{primary}, cleanupErrors...)...)
}
