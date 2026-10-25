//go:build !windows

package platform

import (
	"duoreader/internal/app"
	"errors"
)

func Run(url, data string, s *app.Server) error {
	return errors.New("нативные окна доступны в Windows; используйте -serve для браузерного режима")
}
