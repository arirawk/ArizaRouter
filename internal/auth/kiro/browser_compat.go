package kiro

import (
	"os"

	log "github.com/sirupsen/logrus"
)

// kiroIncognitoDefault mirrors the Plus edition default of opening Kiro login
// pages in a private browser window so that a different account can be chosen.
// This core's browser package has no incognito support, so the value only
// drives log output.
const kiroIncognitoDefault = true

// setIncognitoMode is a compatibility shim: the Plus edition switches the
// shared browser helper into incognito mode before opening login URLs. This
// core's internal/browser package does not expose that switch, so the request
// is only logged.
func setIncognitoMode(enabled bool) {
	if enabled {
		log.Debug("kiro: incognito browser mode requested (not supported by this build, opening a normal window)")
	}
}

// closeBrowser is a compatibility shim: this core's browser package does not
// track the spawned browser process, so there is nothing to close.
func closeBrowser() error {
	return nil
}

// isInteractiveTerminal checks if stdin is connected to an interactive terminal.
// Returns false in CI/automated environments or when stdin is piped.
func isInteractiveTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
