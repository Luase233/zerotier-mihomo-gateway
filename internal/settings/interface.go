package settings

import "errors"

func uniqueInterface(matches []uint32) (uint32, error) {
	if len(matches) == 0 {
		return 0, errors.New("configured Windows/phone ZeroTier subnet is not assigned to an active ZeroTier interface")
	}
	if len(matches) != 1 {
		return 0, errors.New("multiple ZeroTier interfaces match configured Windows/phone addresses")
	}
	return matches[0], nil
}
