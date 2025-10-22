package main

import (
	"fmt"
)

type ConvertibleBoolean bool

func (bit *ConvertibleBoolean) UnmarshalJSON(data []byte) error {
	asString := string(data)
	switch asString {
	case "1", "true":
		*bit = true
	case "0", "false":
		*bit = false
	default:
		return fmt.Errorf("boolean unmarshal error: invalid input %s", asString)
	}

	return nil
}
