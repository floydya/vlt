package vaultexec

func LoginEnvironment(address, namespace string) EnvironmentOverlay {
	overlay := EnvironmentOverlay{
		Set: map[string]string{
			"VAULT_ADDR": address,
		},
		Unset: []string{"VAULT_TOKEN"},
	}
	if namespace == "" {
		overlay.Unset = append(overlay.Unset, "VAULT_NAMESPACE")
	} else {
		overlay.Set["VAULT_NAMESPACE"] = namespace
	}
	return overlay
}
