package cleaning

import "unicode/utf8"

func IsTrash(input string) bool {
	if input == "" {
		return true
	}
	if utf8.RuneCountInString(input) < 8 {
		return true
	}
	return false
}
