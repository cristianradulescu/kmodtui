package kmod

import (
	"fmt"
	"regexp"
)

var (
	// A leading '-' is refused as well, so a name can never be read as an
	// option by modprobe.
	moduleNameRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]*$`)
	paramNameRe  = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	// No spaces, quotes, '=', '#' or newlines: values end up on an
	// "options" line in modprobe.d and in sysfs.
	paramValueRe = regexp.MustCompile(`^[A-Za-z0-9_.,:+/-]+$`)
)

// Kernel limits: MODULE_NAME_LEN is 56; parameters have no hard limit, but
// nothing legitimate comes close to these.
const (
	maxModuleName = 56
	maxParamName  = 64
	maxParamValue = 256
)

// ValidModuleName checks the syntax of a module name.
func ValidModuleName(name string) error {
	if len(name) > maxModuleName || !moduleNameRe.MatchString(name) {
		return fmt.Errorf("invalid module name %q: use letters, digits, '_' and '-' (not first), at most %d characters", name, maxModuleName)
	}
	return nil
}

// ValidParamName checks the syntax of a module parameter name.
func ValidParamName(name string) error {
	if len(name) > maxParamName || !paramNameRe.MatchString(name) {
		return fmt.Errorf("invalid parameter name %q: use letters, digits and '_'", name)
	}
	return nil
}

// ValidParamValue checks a parameter value.
func ValidParamValue(v string) error {
	if len(v) > maxParamValue || !paramValueRe.MatchString(v) {
		return fmt.Errorf("invalid parameter value %q: use letters, digits and . , : + / - _ (no spaces), at most %d characters", v, maxParamValue)
	}
	return nil
}
