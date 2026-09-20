//go:build windows

package securefile

import "os"

func validatePrivate(os.FileInfo, string) error {
	return nil
}

func syncDirectory(*os.Root) error {
	return nil
}
