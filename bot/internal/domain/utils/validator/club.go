package validator

import (
	"net/url"
	"strconv"
	"unicode/utf8"
)

func ClubName(name string, _ map[string]interface{}) bool {
	return utf8.RuneCountInString(name) >= 3 && utf8.RuneCountInString(name) <= 30
}

func ClubDescription(description string, _ map[string]interface{}) bool {
	return utf8.RuneCountInString(description) <= 500
}

func ClubLink(link string, _ map[string]interface{}) bool {
	if _, err := url.ParseRequestURI(link); err != nil {
		return false
	}

	return utf8.RuneCountInString(link) <= 100
}

func ChannelID(id string, _ map[string]interface{}) bool {
	_, err := strconv.ParseInt(id, 10, 64)
	return err == nil
}
