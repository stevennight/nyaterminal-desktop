//go:build !windows

package app

import (
	"errors"

	"github.com/nyaterminal/nyaterminal-desktop/internal/model"
)

func (a *App) launchRDP(_ model.Connection, _ model.Credential) error {
	return errors.New("launching Remote Desktop is only supported on Windows")
}
