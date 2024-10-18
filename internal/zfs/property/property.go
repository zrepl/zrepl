package property

import (
	"fmt"
	"regexp"
)

type Property string

// Check property name conforms to zfsprops(8), section "User Properties"
// Keep regex and error message in sync!
var (
	propertyValidNameChars    = regexp.MustCompile(`^[0-9a-zA-Z-_\.:]+$`)
	propertyValidNameCharsErr = fmt.Errorf("property name must only contain alphanumeric chars and any in %q", "-_.:")
)

func (p Property) Validate() error {
	const PROPERTYNAMEMAXLEN int = 256

	if len(p) < 1 {
		return fmt.Errorf("property name cannot be empty")
	}
	if len(p) > PROPERTYNAMEMAXLEN {
		return fmt.Errorf("property name longer than %d characters", PROPERTYNAMEMAXLEN)
	}
	if p[0] == '-' {
		return fmt.Errorf("property name cannot start with '-'")
	}
	if !propertyValidNameChars.MatchString(string(p)) {
		return propertyValidNameCharsErr
	}
	return nil
}
